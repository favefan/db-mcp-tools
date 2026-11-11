package storage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dbmcp.local/server/internal/model"
)

func TestRequestHistoryEncryptionRetentionAndReadOnlyLegacyFallback(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	detail := model.RequestDetail{ID: "request-history", SQL: "SELECT 'private-literal'", ParameterJSON: `[9007199254740993,"private-parameter"]`, Status: "pending", TraceID: "trace-id", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err = s.RecordRequestDetail(detail); err != nil {
		t.Fatal(err)
	}
	changed := detail
	changed.SQL = "changed retry"
	if err = s.RecordRequestDetail(changed); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteRequestDetail(detail.ID, "succeeded", model.ExecutionSummary{RowCount: 2, Affected: 0, Masks: []model.MaskMatch{{ID: "rule", Target: "notes", Mode: "fixed", Layer: "instance"}}}, 42); err != nil {
		t.Fatal(err)
	}
	for _, event := range []model.Audit{{RequestID: detail.ID, Action: "received", Decision: "pending"}, {RequestID: "unrelated", Action: "unrelated"}, {RequestID: detail.ID, Action: "execute", Decision: "succeeded"}} {
		if err = s.Audit(event); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.Audits("", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.AuditDetail(events[0].ID)
	if err != nil || v.Request == nil || v.Request.SQL != detail.SQL || v.Request.ParameterJSON != detail.ParameterJSON || v.Request.Result.RowCount != 2 || len(v.Events) != 2 || v.Events[0].ID >= v.Events[1].ID {
		t.Fatalf("detail mismatch: %#v %v", v, err)
	}
	var encrypted []byte
	if err = s.DB.QueryRow("SELECT body FROM request_details WHERE request_id=?", detail.ID).Scan(&encrypted); err != nil || strings.Contains(string(encrypted), "private-") {
		t.Fatal("history not encrypted", err)
	}
	if _, err = s.DB.Exec("UPDATE request_details SET created_at=?", time.Now().Add(-31*24*time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err = s.Cleanup(); err != nil {
		t.Fatal(err)
	}
	v, err = s.AuditDetail(events[0].ID)
	if err != nil || v.Request != nil || v.PayloadState != "not_recorded" {
		t.Fatal("old history retained", err)
	}

	e, _ := s.SaveEntity(model.Entity{Name: "legacy", Enabled: true})
	c, _, _ := s.Issue(e.ID, "")
	i, _ := s.SaveInstance(model.Instance{Name: "legacy"})
	q := model.Query{SQL: "SELECT $1", Params: []any{json.Number("9007199254740993")}, Tool: "db_query"}
	r := model.Request{ID: model.ID(), EntityID: e.ID, CredentialID: c.ID, InstanceID: i.ID, ClientRequestID: "legacy", Digest: "legacy", Status: "pending", ExpiresAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}
	if _, _, err = s.CreateRequest(r, q); err != nil {
		t.Fatal(err)
	}
	if err = s.Audit(model.Audit{RequestID: r.ID}); err != nil {
		t.Fatal(err)
	}
	events, _ = s.Audits("", "", "", 0)
	v, err = s.AuditDetail(events[0].ID)
	if err != nil || v.PayloadState != "available" || v.Request.ParameterJSON != `[9007199254740993]` {
		t.Fatal("legacy payload not readable", err)
	}
	var status string
	var payload []byte
	if err = s.DB.QueryRow("SELECT status,payload FROM requests WHERE id=?", r.ID).Scan(&status, &payload); err != nil || status != "pending" || len(payload) == 0 {
		t.Fatal("detail mutated active execution state", err)
	}
	if _, err = s.DB.Exec("UPDATE requests SET payload=NULL WHERE id=?", r.ID); err != nil {
		t.Fatal(err)
	}
	v, err = s.AuditDetail(events[0].ID)
	if err != nil || v.PayloadState != "cleared" || v.Request.SQL != "" {
		t.Fatal("legacy unavailable content not explicit", err)
	}
}
