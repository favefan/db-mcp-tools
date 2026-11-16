package app

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
)

func TestUnavailableDatabaseFailsBeforeApprovalAndAlertsWithoutSQL(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s)
	defer a.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close() // A reserved local fixture endpoint, never a business database.
	policy := model.DefaultPolicy()
	policy.Approval = "writes"
	i, err := s.SaveInstance(model.Instance{Name: "unavailable", Enabled: true, Driver: "sqlserver", Policy: policy, Connection: model.Connection{Host: "127.0.0.1", Port: port, Username: "fixture", TLS: "disable", DatabaseMode: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	entity, _ := s.SaveEntity(model.Entity{Name: "agent", Enabled: true})
	credential, token, _ := s.Issue(entity.ID, "")
	if _, err = s.SaveBinding(model.Binding{EntityID: entity.ID, InstanceID: i.ID, Policy: model.DefaultPolicy()}); err != nil {
		t.Fatal(err)
	}
	identity, err := s.Authenticate(token, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := a.Subscribe()
	defer unsubscribe()
	q := model.Query{InstanceID: i.ID, Database: "fixture", Tool: "db_execute", SQL: "INSERT INTO dbo.private_table VALUES ('secret')", ClientRequestID: "offline-write"}
	r, err := a.Query(context.Background(), identity, q)
	if err == nil || r.Status != "failed" {
		t.Fatalf("database failure reported as denial: %#v %v", r, err)
	}
	rows, err := s.AuditRequestsRange(entity.ID, i.ID, "failed", 0, "", "")
	if err != nil || len(rows) != 1 || rows[0].Decision != "failed" {
		t.Fatalf("failure audit: %#v %v", rows, err)
	}
	detail, err := s.AuditDetail(rows[0].ID)
	if err != nil || detail.Request.Status != "failed" || detail.Request.Result.Reason == "" {
		t.Fatalf("failure detail: %#v %v", detail, err)
	}
	if pending, err := s.Requests(); err != nil || len(pending) != 0 {
		t.Fatalf("offline SQL became approvable: %#v %v", pending, err)
	}
	var alert Event
	select {
	case alert = <-events:
	case <-time.After(time.Second):
		t.Fatal("missing database failure event")
	}
	if alert.Failure == nil || alert.Failure.RequestID != r.RequestID || alert.Failure.Phase != "preparation" {
		t.Fatalf("missing pre-approval alert: %#v", alert)
	}
	encoded, _ := json.Marshal(alert.Failure)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "private_table") {
		t.Fatal("failure alert exposed SQL")
	}
	// An approved request must also become terminal if connectivity disappears
	// during recheck, so repeated resumes cannot later execute it silently.
	q.ClientRequestID = "approved-before-disconnect"
	v := model.Request{ID: model.ID(), EntityID: entity.ID, CredentialID: credential.ID, InstanceID: i.ID, Database: q.Database, ClientRequestID: q.ClientRequestID, Digest: digest(q), Status: "approved", CreatedAt: time.Now().UTC().Format(time.RFC3339), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	if _, _, err = s.CreateRequest(v, q); err != nil {
		t.Fatal(err)
	}
	a.recordRequestDetail(context.Background(), identity, q, v)
	r, err = a.Resume(context.Background(), identity, v.ID)
	if err == nil || r.Status != "failed" {
		t.Fatalf("recheck failure: %#v %v", r, err)
	}
	r, err = a.Resume(context.Background(), identity, v.ID)
	if err != nil || r.Status != "failed" {
		t.Fatalf("failed request replayed: %#v %v", r, err)
	}
	// Permission denials still stay denied and do not generate database alerts.
	entity.Enabled = false
	if _, err = s.SaveEntity(entity); err != nil {
		t.Fatal(err)
	}
	for len(events) > 0 {
		<-events
	}
	r, err = a.Query(context.Background(), identity, q)
	if err == nil || r.Status != "denied" {
		t.Fatalf("hard denial changed: %#v %v", r, err)
	}
	for len(events) > 0 {
		if event := <-events; event.Failure != nil {
			t.Fatal("permission denial emitted database alert")
		}
	}
}
