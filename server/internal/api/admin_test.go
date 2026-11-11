package api

import (
	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/storage"
	"net/http/httptest"
	"testing"
)

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
