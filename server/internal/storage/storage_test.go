package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dbmcp.local/server/internal/model"
)

func TestCredentialIsolationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.SaveEntity(model.Entity{Name: "agent", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c, token, err := s.Issue(e.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(s.AdminToken, "127.0.0.1"); err == nil {
		t.Fatal("Admin token authenticated as an entity")
	}
	id, err := s.Authenticate(token, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(c.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Recheck(id); err == nil {
		t.Fatal("revocation ignored")
	}
	_, token, err = s.Issue(e.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	e.Enabled = false
	if _, err = s.SaveEntity(e); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(token, "127.0.0.1"); err == nil {
		t.Fatal("disabled entity accepted")
	}
	serverID := s.ServerID
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.ServerID != serverID {
		t.Fatal("server identity changed")
	}
	if _, err = s.Authenticate(token, "127.0.0.1"); err == nil {
		t.Fatal("disabled state lost on restart")
	}
}

func TestInitializationPreservesUnrelatedDirectoryContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unrelated.txt")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(dir); err == nil {
		s.Close()
		t.Fatal("initialized an unrelated nonempty directory")
	}
	bytes, err := os.ReadFile(path)
	if err != nil || string(bytes) != "keep" {
		t.Fatal("existing content changed")
	}
	if _, err = os.Stat(filepath.Join(dir, "admin.token")); !os.IsNotExist(err) {
		t.Fatal("created secrets in an unrelated directory")
	}
}

func TestRequestClaimAndOnlineBackup(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err := s.SaveEntity(model.Entity{Name: "agent", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := s.Issue(e.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	i, err := s.SaveInstance(model.Instance{Name: "database", Connection: model.Connection{Password: "private-db-password"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SaveBinding(model.Binding{EntityID: e.ID, InstanceID: i.ID})
	if err != nil || b.Revision != 1 {
		t.Fatalf("binding %v %v", b, err)
	}
	q := model.Query{SQL: "UPDATE users SET name=$1", Params: []any{"private-value"}}
	r := model.Request{ID: model.ID(), EntityID: e.ID, CredentialID: c.ID, InstanceID: i.ID, ClientRequestID: "retry", Digest: "digest", Status: "approved", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339)}
	_, created, err := s.CreateRequest(r, q)
	if err != nil || !created {
		t.Fatal(err)
	}
	_, created, err = s.CreateRequest(r, q)
	if err != nil || created {
		t.Fatal("idempotent request failed")
	}
	r.Digest = "changed"
	if _, _, err = s.CreateRequest(r, q); err != ErrConflict {
		t.Fatal("changed payload was accepted")
	}
	var count atomic.Int32
	loaded, err := s.Request(r.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if s.Claim(loaded) == nil {
				count.Add(1)
			}
		})
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("claims=%d", count.Load())
	}
	var body string
	if err = s.DB.QueryRow("SELECT body FROM requests WHERE id=?", r.ID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "private-value") || strings.Contains(body, "UPDATE users") {
		t.Fatal("claim persisted decrypted request data")
	}
	expired := r
	expired.ID = model.ID()
	expired.ClientRequestID = "expired"
	expired.Status = "pending"
	expired.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
	if _, _, err = s.CreateRequest(expired, q); err != nil {
		t.Fatal(err)
	}
	if s.Transition(expired.ID, "pending", "approved") == nil || s.Claim(expired) == nil {
		t.Fatal("expired approval was approved or claimed")
	}
	currentExpired, err := s.Request(expired.ID, true)
	if err != nil || currentExpired.Status != "expired" || currentExpired.Query != nil {
		t.Fatalf("expired request retained executable payload: %#v %v", currentExpired, err)
	}
	backup, err := s.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	current, err := snapshot.Request(r.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "result_unknown" {
		t.Fatalf("interrupted write status=%s", current.Status)
	}
	if current.Query.SQL != q.SQL {
		t.Fatal("encrypted payload not restored")
	}
	data, err := os.ReadFile(filepath.Join(backup, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-db-password", "private-value"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("plaintext secret in backup")
		}
	}
}
