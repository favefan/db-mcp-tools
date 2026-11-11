package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
)

func TestAuditDetailsAreAdminOnlyAndDeniedQueriesAreTraceable(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	e, err := s.SaveEntity(model.Entity{Name: "fixture", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c, token, err := s.Issue(e.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	q := model.Query{InstanceID: "missing", SQL: "SELECT 'audit-private-sql'", Params: []any{json.Number("9007199254740993")}, Tool: "db_query", ClientRequestID: "rejected"}
	if _, err = a.Query(t.Context(), model.Identity{EntityID: e.ID, CredentialID: c.ID, Source: "127.0.0.1"}, q); err == nil {
		t.Fatal("unbound query accepted")
	}
	events, err := s.Audits("", "", "", 0)
	if err != nil || len(events) == 0 || events[0].RequestID == "" {
		t.Fatal("denied query untraceable", err)
	}
	h := Admin(a, func() {}, "foreground")
	for _, tc := range []struct {
		path, token string
		status      int
	}{{"1", token, 401}, {"1", "", 401}, {"999999", s.AdminToken, 404}, {"bad", s.AdminToken, 400}, {"1", s.AdminToken, 200}} {
		req := httptest.NewRequest("GET", "/admin/v1/audit-events/"+tc.path, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Authorization", "Bearer "+tc.token)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if out.Code != tc.status {
			t.Fatalf("detail status %s: %d", tc.path, out.Code)
		}
		if tc.status == 200 {
			var v model.AuditDetail
			if json.Unmarshal(out.Body.Bytes(), &v) != nil || v.Request == nil || v.Request.SQL != q.SQL || v.Request.Status != "denied" || v.Request.ParameterJSON != `[9007199254740993]` || out.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("denied detail missing", out.Body.String())
			}
			if strings.Contains(out.Body.String(), token) {
				t.Fatal("credential leaked")
			}
		}
	}
}
