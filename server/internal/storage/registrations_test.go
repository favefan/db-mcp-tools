package storage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"dbmcp.local/server/internal/model"
)

func TestFirstUseRegistrationIsStableAndCannotBypassApproval(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, token, err := s.IssueRegistration("Codex workspace", "")
	if err != nil {
		t.Fatal(err)
	}
	entities, _ := s.Entities()
	if len(entities) != 0 {
		t.Fatal("issuing registration pre-created an entity")
	}
	for _, unknown := range []string{"", s.AdminToken, "unknown"} {
		if _, err = s.Authenticate(unknown, "127.0.0.1"); !errors.Is(err, ErrDenied) {
			t.Fatalf("unknown credential accepted: %v", err)
		}
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for range 8 {
		wg.Go(func() {
			id, e := s.Authenticate(token, "127.0.0.1")
			if !errors.Is(e, ErrRegistrationPending) {
				t.Errorf("first use: %v", e)
			}
			ids <- id.EntityID
		})
	}
	wg.Wait()
	close(ids)
	var entityID string
	for id := range ids {
		if entityID == "" {
			entityID = id
		}
		if id == "" || id != entityID {
			t.Fatal("concurrent first use created different entities")
		}
	}
	entities, _ = s.Entities()
	if len(entities) != 1 || entities[0].Enabled || entities[0].Notes != "" {
		t.Fatal("pending entity must be unique and disabled")
	}
	e := entities[0]
	e.Enabled = true
	e, err = s.SaveEntity(e)
	if err != nil {
		t.Fatal(err)
	}
	c, extraToken, err := s.Issue(e.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(extraToken, "127.0.0.1"); !errors.Is(err, ErrRegistrationPending) {
		t.Fatal("another credential bypassed pending registration")
	}
	if err = s.Recheck(model.Identity{EntityID: e.ID, CredentialID: c.ID}); !errors.Is(err, ErrRegistrationPending) {
		t.Fatal("recheck bypassed pending registration")
	}
	p := model.DefaultBindingPolicy()
	instance, err := s.SaveInstance(model.Instance{Name: "configured only", Enabled: true, Policy: model.DefaultPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	binding := model.Binding{InstanceID: instance.ID, Policy: p}
	if _, err = s.SaveBinding(model.Binding{EntityID: e.ID, InstanceID: instance.ID, Policy: p}); !errors.Is(err, ErrRegistrationPending) {
		t.Fatal("pending entity bound outside registration approval")
	}
	if _, _, err = s.Rotate(r.ID); !errors.Is(err, ErrRegistrationPending) {
		t.Fatal("pending registration credential rotated outside approval")
	}
	if _, err = s.ApproveRegistration(r.ID, []model.Binding{binding, {InstanceID: "missing", Policy: p}}); err == nil {
		t.Fatal("partial approval accepted")
	}
	bs, _ := s.Bindings(e.ID)
	if len(bs) != 0 {
		t.Fatal("failed approval left a binding")
	}
	if _, err = s.Authenticate(token, "127.0.0.1"); !errors.Is(err, ErrRegistrationPending) {
		t.Fatal("failed approval granted access")
	}
	approved, err := s.ApproveRegistration(r.ID, []model.Binding{binding})
	if err != nil || approved.Status != "approved" {
		t.Fatalf("approve: %v", err)
	}
	id, err := s.Authenticate(token, "192.0.2.20")
	if err != nil || id.EntityID != entityID {
		t.Fatal("approved identity changed with source or credential")
	}
	if _, err = s.ApproveRegistration(r.ID, []model.Binding{binding}); !errors.Is(err, ErrConflict) {
		t.Fatal("approval replay accepted")
	}
	if err = s.RevokeRegistration(r.ID, "revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(extraToken, "127.0.0.1"); !errors.Is(err, ErrDenied) {
		t.Fatal("revocation left another credential active")
	}
	if _, err = s.Authenticate(token, "127.0.0.1"); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked token re-registered")
	}
	all, _ := s.Registrations()
	if len(all) != 1 || all[0].Status != "revoked" {
		t.Fatal("registration state lost")
	}
	var body string
	if err = s.DB.QueryRow("SELECT body FROM registrations WHERE id=?", r.ID).Scan(&body); err != nil || strings.Contains(body, token) {
		t.Fatal("registration exposed plaintext token")
	}
	denied, deniedToken, err := s.IssueRegistration("Claude workspace", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(deniedToken, "127.0.0.1"); !errors.Is(err, ErrRegistrationPending) {
		t.Fatal(err)
	}
	if err = s.RevokeRegistration(denied.ID, "denied"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(deniedToken, "127.0.0.1"); !errors.Is(err, ErrDenied) {
		t.Fatal("denied token re-registered")
	}
	unused, unusedToken, err := s.IssueRegistration("unused invitation", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(unused.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(unusedToken, "127.0.0.1"); !errors.Is(err, ErrDenied) {
		t.Fatal("unused revoked invitation registered")
	}
	path, err := s.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	info, err := ValidateBackup(path)
	if err != nil || info.Schema != SchemaVersion {
		t.Fatalf("new backup schema: %v", err)
	}
}

func TestRegistrationNoteMigrationPreservesApprovalAndUserNotes(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	const legacyNote = "Agent 首次接入登记，等待管理员授权"
	wantNotes := map[string]string{}
	var tokens []string
	instance, err := s.SaveInstance(model.Instance{Name: "no database connection", Enabled: true, Policy: model.DefaultPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	for _, note := range []string{legacyNote, "管理员填写的备注"} {
		r, token, err := s.IssueRegistration("approved agent", "")
		if err != nil {
			t.Fatal(err)
		}
		id, err := s.Authenticate(token, "127.0.0.1")
		if !errors.Is(err, ErrRegistrationPending) {
			t.Fatal(err)
		}
		if _, err = s.ApproveRegistration(r.ID, []model.Binding{{InstanceID: instance.ID, Policy: model.DefaultBindingPolicy()}}); err != nil {
			t.Fatal(err)
		}
		e, err := s.Entity(id.EntityID)
		if err != nil {
			t.Fatal(err)
		}
		e.Notes = note
		if _, err = s.SaveEntity(e); err != nil {
			t.Fatal(err)
		}
		wantNotes[e.ID] = note
		if note == legacyNote {
			wantNotes[e.ID] = ""
		}
		tokens = append(tokens, token)
	}
	unrelated, err := s.SaveEntity(model.Entity{Name: "manual entity", Notes: legacyNote, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	wantNotes[unrelated.ID] = legacyNote
	if _, err = s.DB.Exec("DROP TABLE request_details; DROP INDEX audit_request"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("DELETE FROM goose_db_version WHERE version_id>=5"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	entities, err := s.Entities()
	if err != nil || len(entities) != 3 {
		t.Fatal("entities changed during migration", err)
	}
	for _, e := range entities {
		if !e.Enabled || e.Notes != wantNotes[e.ID] {
			t.Fatal("obsolete note remains or unrelated state changed", e)
		}
	}
	for _, token := range tokens {
		if _, err = s.Authenticate(token, "127.0.0.1"); err != nil {
			t.Fatal("approved credential changed", err)
		}
	}
	registrations, err := s.Registrations()
	if err != nil || len(registrations) != 2 {
		t.Fatal(err)
	}
	for _, r := range registrations {
		bindings, err := s.Bindings(r.EntityID)
		if err != nil || r.Status != "approved" || len(bindings) != 1 {
			t.Fatal("approval or bindings changed", err)
		}
	}
	backups, err := s.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatal("migration backup missing", err)
	}
	info, err := ValidateBackup(backups[0].Path)
	if err != nil || info.Schema != 4 {
		t.Fatal("pre-upgrade backup not schema 4", err)
	}
}

func TestRegistrationMigrationBacksUpExistingSchema(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	entity, err := s.SaveEntity(model.Entity{Name: "existing", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := s.Issue(entity.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	serverID := s.ServerID
	if _, err = s.DB.Exec("DROP TABLE registrations"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("DROP TABLE request_details; DROP INDEX audit_request"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("DELETE FROM goose_db_version WHERE version_id>=2"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.Authenticate(token, "127.0.0.1")
	if err != nil || id.EntityID != entity.ID || s.ServerID != serverID {
		t.Fatal("migration changed identity or credential")
	}
	backups, err := s.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("pre-migration snapshot: %v", err)
	}
	info, err := ValidateBackup(backups[0].Path)
	if err != nil || info.Schema != 1 {
		t.Fatalf("old backup not restorable: %v", err)
	}
}
