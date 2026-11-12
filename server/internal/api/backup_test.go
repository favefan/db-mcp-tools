package api

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/storage"
)

func TestBackupDeletionRequiresAdministratorAndCorrectServer(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	handler := Admin(a, func() {}, "foreground")
	call := func(method, path, token, server string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/admin/v1/"+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-DBMCP-Server-ID", server)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	for range 2 {
		out := call("POST", "backups", s.AdminToken, s.ServerID)
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
	}
	out := call("GET", "backups", s.AdminToken, s.ServerID)
	var backups []storage.BackupInfo
	if err := json.Unmarshal(out.Body.Bytes(), &backups); err != nil || len(backups) != 2 {
		t.Fatal(err, out.Body.String())
	}
	id := backups[0].ID
	if id == "" {
		t.Fatal("missing server-issued backup ID")
	}
	for _, test := range []struct {
		token, server string
		status        int
	}{
		{"", s.ServerID, 401},
		{"entity-credential", s.ServerID, 401},
		{s.AdminToken, "another-server", 403},
	} {
		out := call("DELETE", "backups/"+id, test.token, test.server)
		if out.Code != test.status {
			t.Fatal("unexpected authorization result", out.Code, out.Body.String())
		}
		if _, err := storage.ValidateBackup(backups[0].Path); err != nil {
			t.Fatal("unauthorized deletion changed snapshot", err)
		}
	}
	out = call("DELETE", "backups/"+url.PathEscape("../state.sqlite"), s.AdminToken, s.ServerID)
	if out.Code != 400 {
		t.Fatal("unsafe backup ID was accepted", out.Code)
	}
	out = call("DELETE", "backups/"+id, s.AdminToken, s.ServerID)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	out = call("DELETE", "backups/"+id, s.AdminToken, s.ServerID)
	if out.Code != 404 {
		t.Fatal("repeat deletion should be not found", out.Code)
	}
	remaining, err := s.Backups()
	if err != nil || len(remaining) != 1 || remaining[0].ID != backups[1].ID {
		t.Fatal(remaining, err)
	}
	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM audit_events WHERE action='backup.delete' AND detail=?", id).Scan(&count); err != nil || count != 1 {
		t.Fatal("deletion was not audited once", count, err)
	}
}
