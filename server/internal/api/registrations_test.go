package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
)

func TestRegistrationBlocksMCPAndRequiresAuthenticatedApproval(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	admin := Admin(a, func() {}, "foreground")
	mcp := MCP(a)
	r, token, err := s.IssueRegistration("first connection", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "fixture-agent")
	out := httptest.NewRecorder()
	mcp.ServeHTTP(out, req)
	if out.Code != http.StatusForbidden || !strings.Contains(out.Body.String(), "entity_pending") {
		t.Fatalf("first MCP request: %d %s", out.Code, out.Body.String())
	}
	entities, _ := s.Entities()
	if len(entities) != 1 || entities[0].Client != "fixture-agent" {
		t.Fatal("first request was not observed")
	}
	instance, err := s.SaveInstance(model.Instance{Name: "no connection", Enabled: true, Policy: model.DefaultPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(model.RegistrationApproval{Bindings: []model.Binding{{InstanceID: instance.ID, Policy: model.DefaultBindingPolicy()}}})
	for _, credential := range []string{token, s.AdminToken} {
		req = httptest.NewRequest("POST", "/admin/v1/registrations/"+r.ID+"/approve", bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+credential)
		out = httptest.NewRecorder()
		admin.ServeHTTP(out, req)
		if credential == token && out.Code != 401 {
			t.Fatal("Agent approved its own registration")
		}
		if credential == s.AdminToken && out.Code != 200 {
			t.Fatalf("admin approval: %d %s", out.Code, out.Body.String())
		}
	}
	if _, err = s.Authenticate(token, "127.0.0.1"); err != nil {
		t.Fatal("same token did not become usable", err)
	}
	for _, payload := range []string{
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture-agent","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_instances","arguments":{}}}`,
	} {
		req = httptest.NewRequest("POST", "/mcp", strings.NewReader(payload))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		out = httptest.NewRecorder()
		mcp.ServeHTTP(out, req)
		if out.Code != 200 || strings.Contains(out.Body.String(), `"error"`) {
			t.Fatalf("MCP after approval: %d %s", out.Code, out.Body.String())
		}
		if strings.Contains(payload, "tools/call") && !strings.Contains(out.Body.String(), instance.ID) {
			t.Fatalf("approved instance missing: %s", out.Body.String())
		}
	}
	req = httptest.NewRequest("POST", "/admin/v1/registrations/"+r.ID+"/approve", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+s.AdminToken)
	out = httptest.NewRecorder()
	admin.ServeHTTP(out, req)
	if out.Code != 409 {
		t.Fatal("repeated approval did not conflict")
	}
}

func TestMaskPreviewOnlyAppliesMatchingSourceRules(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	h := Admin(a, func() {}, "foreground")
	for _, tt := range []struct {
		column, want string
		status       int
	}{{"email", "[EMAIL]", 200}, {"notes", "person@example.com", 200}, {"", "", 400}} {
		body, _ := json.Marshal(map[string]any{"rules": []model.MaskRule{{ID: "mail", Target: "email", Mode: "fixed", Value: "[EMAIL]"}}, "source": model.Source{Schema: "public", Table: "users", Column: tt.column}, "value": "person@example.com"})
		req := httptest.NewRequest("POST", "/admin/v1/mask-preview", bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+s.AdminToken)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if out.Code != tt.status {
			t.Fatalf("source %s: %d %s", tt.column, out.Code, out.Body.String())
		}
		if tt.status == 200 {
			var result struct {
				Value string `json:"value"`
			}
			if json.Unmarshal(out.Body.Bytes(), &result) != nil || result.Value != tt.want {
				t.Fatalf("source %s: %s", tt.column, out.Body.String())
			}
		}
	}
}
