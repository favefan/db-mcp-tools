package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	run "dbmcp.local/server/internal/runtime"
	"dbmcp.local/server/internal/storage"
)

func TestProfilePortsPreserveExistingDeploymentAndSecrets(t *testing.T) {
	dir := t.TempDir()
	first, err := run.InitializePorts(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if first["admin"] == first["mcp"] {
		t.Fatal("listeners share a port")
	}
	files := map[string][]byte{}
	for _, name := range []string{"deployment.toml", "key", "admin.token"} {
		files[name], err = os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Runtime metadata must work while the business SQLite lock is held.
	info, err := run.Info(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if info["admin"] != first["admin"] || info["mcp"] != first["mcp"] {
		t.Fatal("metadata changed addresses")
	}
	s.Close()
	again, err := run.InitializePorts(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if again["server_id"] != first["server_id"] {
		t.Fatal("identity changed")
	}
	for name, before := range files {
		after, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("existing %s changed: %v", name, err)
		}
	}
}

func TestStopWaitsForSQLiteLockReleaseBeforeReturning(t *testing.T) {
	dir := t.TempDir()
	if _, err := run.InitializePorts(dir, true); err != nil {
		t.Fatal(err)
	}
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var stopping atomic.Bool
	accepted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/v1/runtime/stop" {
			stopping.Store(true)
			close(accepted)
			w.Write([]byte(`{}`))
			return
		}
		if stopping.Load() {
			http.Error(w, "stopped", 503)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"server_id": s.ServerID, "data_dir": s.Dir, "mode": "background"})
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	if err = os.WriteFile(filepath.Join(dir, "deployment.toml"), []byte("admin = '"+endpoint.Host+"'\nmcp = '127.0.0.1:1'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run.Stop(ctx, dir) }()
	select {
	case <-accepted:
	case <-ctx.Done():
		t.Fatal("stop was not accepted")
	}
	select {
	case err := <-done:
		t.Fatalf("stop returned before SQLite closed: %v", err)
	case <-time.After(450 * time.Millisecond):
	}
	s.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("stop did not finish after lock release")
	}
}
