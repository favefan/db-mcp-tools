package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dbmcp.local/server/internal/model"
	run "dbmcp.local/server/internal/runtime"
	"dbmcp.local/server/internal/storage"
)

func TestRestoreKeepsSecretsAndRequiresStoppedServer(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "恢复 空格")
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	i, err := s.SaveInstance(model.Instance{Name: "original", Connection: model.Connection{Password: "original-secret"}, Enabled: true, Policy: model.DefaultPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	serverID := s.ServerID
	backup, err := s.Backup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = run.Restore(ctx, dir, backup); err == nil {
		t.Fatal("restore acquired a live Server directory")
	}
	i.Name = "changed"
	if _, err = s.SaveInstance(i); err != nil {
		t.Fatal(err)
	}
	s.Close()
	result, err := run.Restore(ctx, dir, backup)
	if err != nil {
		t.Fatal(err)
	}
	if result["previous_backup"] == "" {
		t.Fatal("missing recovery snapshot")
	}
	s, err = storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := s.Instance(i.ID, true)
	if err != nil || restored.Name != "original" || restored.Connection.Password != "original-secret" || s.ServerID != serverID {
		t.Fatalf("restore lost identity or secrets: %v", err)
	}
	s.Close()
	if err = os.WriteFile(filepath.Join(backup, "key"), make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = run.Restore(ctx, dir, backup); err == nil {
		t.Fatal("mismatched backup key accepted")
	}
	s, err = storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Instance(i.ID, true); err != nil {
		t.Fatal("rejected restore changed active encryption key")
	}
}
