package storage

import (
	"testing"
	"time"

	"dbmcp.local/server/internal/model"
)

func TestAuditRequestSummaryFollowsStateWithoutLosingTimeline(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entity, _ := s.SaveEntity(model.Entity{Name: "agent", Enabled: true})
	credential, _, _ := s.Issue(entity.ID, "")
	instance, _ := s.SaveInstance(model.Instance{Name: "test"})
	request := model.Request{ID: model.ID(), EntityID: entity.ID, CredentialID: credential.ID, InstanceID: instance.ID, ClientRequestID: "summary", Digest: "digest", Status: "pending", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	if _, _, err = s.CreateRequest(request, model.Query{}); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordRequestDetail(model.RequestDetail{ID: request.ID, AgentReason: "核查订单状态，读取订单数据。", CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	add := func(action, decision string) {
		t.Helper()
		if err := s.Audit(model.Audit{EntityID: entity.ID, InstanceID: instance.ID, RequestID: request.ID, Action: action, Decision: decision, DurationMS: 42}); err != nil {
			t.Fatal(err)
		}
	}
	add("request.received", "received")
	add("db_query", "pending")
	assertState := func(status string) []model.Audit {
		t.Helper()
		rows, err := s.AuditRequestsRange(entity.ID, instance.ID, status, 0, "", "")
		if err != nil || len(rows) != 1 || rows[0].RequestStatus != status {
			t.Fatalf("summary %s: %#v %v", status, rows, err)
		}
		return rows
	}
	assertState("pending")
	if err = s.Transition(request.ID, "pending", "approved"); err != nil {
		t.Fatal(err)
	}
	add("approval", "approved")
	assertState("approved")
	if err = s.Claim(request); err != nil {
		t.Fatal(err)
	}
	add("execution.start", "executing")
	assertState("executing")
	if err = s.Finish(request.ID, model.Result{Status: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	add("execute", "succeeded")
	rows := assertState("succeeded")
	if rows[0].DurationMS != 42 {
		t.Fatal("execution duration lost")
	}
	if old, err := s.AuditRequestsRange("", "", "pending", 0, "", ""); err != nil || len(old) != 0 {
		t.Fatal("completed request still pending", old, err)
	}
	if old, err := s.AuditRequestsRange("", "", "", rows[0].ID, "", ""); err != nil || len(old) != 0 {
		t.Fatal("pagination resurrected intermediate events", old, err)
	}
	detail, err := s.AuditDetail(rows[0].ID)
	if err != nil || len(detail.Events) != 5 || detail.Events[0].Decision != "received" || detail.Request.AgentReason == "" {
		t.Fatal("timeline/reason lost", detail, err)
	}
	approvals, err := s.Requests()
	if err != nil || len(approvals) != 1 || approvals[0].AgentReason != detail.Request.AgentReason {
		t.Fatal("approval reason lost", approvals, err)
	}
	for range 2 {
		if err = s.Audit(model.Audit{Action: "instance.save", Decision: "saved"}); err != nil {
			t.Fatal(err)
		}
	}
	admins, err := s.AuditRequestsRange("__admin__", "", "", 0, "", "")
	if err != nil || len(admins) != 2 {
		t.Fatal("administrator filter or independent events lost", admins, err)
	}
	// Reads derive expiry without mutating the execution state.
	if _, err = s.DB.Exec("UPDATE requests SET status='pending',expires_at=? WHERE id=?", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), request.ID); err != nil {
		t.Fatal(err)
	}
	assertState("expired")
	var status string
	if err = s.DB.QueryRow("SELECT status FROM requests WHERE id=?", request.ID).Scan(&status); err != nil || status != "pending" {
		t.Fatal("audit read changed state", status, err)
	}
}
