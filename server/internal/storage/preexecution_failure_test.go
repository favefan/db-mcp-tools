package storage

import (
	"errors"
	"testing"
	"time"

	"dbmcp.local/server/internal/model"
)

func TestPreexecutionFailureCannotOverwriteClaimedWrite(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entity, _ := s.SaveEntity(model.Entity{Name: "agent", Enabled: true})
	credential, _, _ := s.Issue(entity.ID, "")
	instance, _ := s.SaveInstance(model.Instance{Name: "test"})
	v := model.Request{ID: model.ID(), EntityID: entity.ID, CredentialID: credential.ID, InstanceID: instance.ID, ClientRequestID: "claimed-write", Digest: "digest", Status: "approved", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	if _, _, err = s.CreateRequest(v, model.Query{SQL: "INSERT INTO fixture VALUES (1)"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Claim(v); err != nil {
		t.Fatal(err)
	}
	if err = s.FailBeforeExecution(v.ID, model.Result{Status: "failed"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("claimed write overwritten: %v", err)
	}
	if err = s.Finish(v.ID, model.Result{Status: "succeeded", Affected: 1}); err != nil {
		t.Fatal(err)
	}
	if err = s.FailBeforeExecution(v.ID, model.Result{Status: "failed"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("completed write overwritten: %v", err)
	}
}
