package api

import (
	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/storage"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestStopRefusesAReplacedProcess(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	stopped := make(chan struct{}, 1)
	handler := Admin(a, func() { stopped <- struct{}{} }, "background")
	health := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+s.AdminToken)
	handler.ServeHTTP(health, req)
	var info map[string]any
	if err = json.Unmarshal(health.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	id, ok := info["instance_id"].(string)
	if !ok || id == "" {
		t.Fatal("missing process identity")
	}
	for _, tt := range []struct {
		id   string
		code int
	}{{"old-process", 403}, {id, 200}} {
		req := httptest.NewRequest("POST", "/admin/v1/runtime/stop", nil)
		req.Header.Set("Authorization", "Bearer "+s.AdminToken)
		req.Header.Set("X-DBMCP-Server-ID", s.ServerID)
		req.Header.Set("X-DBMCP-Instance-ID", tt.id)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tt.code {
			t.Fatalf("stop with %s: %d", tt.id, out.Code)
		}
	}
}

func TestAdminServerIdentity(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	handler := Admin(a, func() {}, "foreground")
	for _, tt := range []struct {
		id   string
		code int
	}{{s.ServerID, 200}, {"different-server", 403}} {
		req := httptest.NewRequest("GET", "/admin/v1/health", nil)
		req.Header.Set("Authorization", "Bearer "+s.AdminToken)
		req.Header.Set("X-DBMCP-Server-ID", tt.id)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tt.code {
			t.Fatalf("server identity %s: got %d", tt.id, out.Code)
		}
	}
}
