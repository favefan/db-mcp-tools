package storage

import (
	"slices"
	"testing"
	"time"

	"dbmcp.local/server/internal/model"
)

func TestObjectScopeMigrationPreservesLegacyScopeAndBackup(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := model.DefaultPolicy()
	p.Resources = []string{"public.users", "private.*"}
	i, err := s.SaveInstance(model.Instance{Name: "legacy selected", Connection: model.Connection{DatabaseMode: "selected", Databases: []string{"first", "second"}}, Policy: p})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("DROP TABLE request_details; DROP INDEX audit_request"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("DELETE FROM goose_db_version WHERE version_id>=4"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Instance(i.ID, true)
	if err != nil || got.Connection.Database != "first" || got.Connection.DatabaseMode != "selected" || !slices.Equal(got.Connection.Databases, i.Connection.Databases) || !slices.Equal(got.Policy.Resources, p.Resources) {
		t.Fatalf("scope changed during migration: %#v %v", got, err)
	}
	backups, err := s.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatal("no schema 3 backup", err)
	}
	info, err := ValidateBackup(backups[0].Path)
	if err != nil || info.Schema != 3 {
		t.Fatal("invalid schema 3 backup", err)
	}
}

func TestDatabaseMigrationPinsLegacyApprovalsAndBacksUpSchemaTwo(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	i, err := s.SaveInstance(model.Instance{Name: "legacy", Connection: model.Connection{Database: "original"}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.SaveEntity(model.Entity{Name: "agent", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := s.Issue(e.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveBinding(model.Binding{EntityID: e.ID, InstanceID: i.ID, Policy: model.DefaultPolicy(), ApprovalRules: []model.ApprovalRule{{ID: "old", Operation: "update", Tables: []model.Source{{Schema: "public", Table: "users"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, revision := range []int{i.Revision, i.Revision + 1} {
		q := model.Query{InstanceID: i.ID, SQL: "UPDATE users SET counter=1", ClientRequestID: model.ID()}
		r := model.Request{ID: model.ID(), EntityID: e.ID, CredentialID: c.ID, InstanceID: i.ID, ClientRequestID: q.ClientRequestID, Digest: Hash(encode(q)), Status: "pending", InstanceRevision: revision, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
		if _, _, err = s.CreateRequest(r, q); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.ID)
	}
	if _, err = s.DB.Exec("DROP TABLE request_details; DROP INDEX audit_request"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("DELETE FROM goose_db_version WHERE version_id>=3"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for index, id := range ids {
		r, err := s.Request(id, true)
		if err != nil || r.Database != "original" {
			t.Fatalf("unpinned request: %#v %v", r, err)
		}
		if index == 0 && (r.Status != "pending" || r.Query == nil || r.Query.Database != "") {
			t.Fatal("legacy encrypted payload changed")
		}
		if index == 1 && (r.Status != "cancelled" || r.Query != nil) {
			t.Fatal("ambiguous old target was not cancelled")
		}
	}
	binding, err := s.Binding(e.ID, i.ID)
	if err != nil || binding.ApprovalRules[0].Database != "original" {
		t.Fatal("legacy remembered scope not pinned", err)
	}
	backups, err := s.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatal("no migration backup", err)
	}
	info, err := ValidateBackup(backups[0].Path)
	if err != nil || info.Schema != 2 {
		t.Fatal("schema 2 backup unavailable", err)
	}
}
