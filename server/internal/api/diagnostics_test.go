package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
)

func TestDiagnosticsRequiresAdminAndNeverExportsSecrets(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	if err = s.RecordRequestDetail(model.RequestDetail{ID: "private-history", SQL: "SELECT SQL_CANARY", ParameterJSON: `["PARAM_CANARY"]`}); err != nil {
		t.Fatal(err)
	}
	policy := model.DefaultPolicy()
	policy.Masks = []model.MaskRule{{ID: "canary", Target: "secret", Mode: "fixed", Value: "MASK_VALUE_CANARY"}}
	_, err = s.SaveInstance(model.Instance{Name: "test", Driver: "postgres", Connection: model.Connection{Host: "127.0.0.1", Port: 1, Password: "PASSWORD_CANARY"}, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Mkdir(filepath.Join(s.Dir, "logs"), 0700)
	_ = os.WriteFile(filepath.Join(s.Dir, "logs", "server-123.jsonl"), []byte("{\"component\":\"server\",\"msg\":\"database.failed\",\"error\":{\"Code\":\"23505\",\"token\":\"TOKEN_CANARY\"}}\n"), 0600)
	_ = os.WriteFile(filepath.Join(s.Dir, "logs", "unrelated.log"), []byte("UNRELATED_CANARY"), 0600)
	handler := Admin(a, func() {}, "foreground")
	for _, token := range []string{"", "agent-token", s.AdminToken} {
		req := httptest.NewRequest("GET", "/admin/v1/diagnostics", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if token != s.AdminToken {
			if out.Code != 401 {
				t.Fatal("non-admin exported diagnostics")
			}
			continue
		}
		if out.Code != 200 {
			t.Fatal(out.Code)
		}
		var snapshot map[string]any
		if json.Unmarshal(out.Body.Bytes(), &snapshot) != nil {
			t.Fatal("invalid snapshot")
		}
		if snapshot["server_id"] != s.ServerID || !strings.Contains(out.Body.String(), "23505") || strings.Contains(out.Body.String(), "CANARY") || strings.Contains(out.Body.String(), s.AdminToken) {
			t.Fatal("snapshot omitted evidence or exposed secret")
		}
	}
}

func TestHTTPTraceCorrelatesFailuresAndContainsNoPayload(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	req := httptest.NewRequest("POST", "/admin/v1/entities?token=QUERY_CANARY", strings.NewReader(`{"name":"","notes":"BODY_CANARY"}`))
	req.Header.Set("Authorization", "Bearer "+s.AdminToken)
	trace := strings.Repeat("a", 32)
	req.Header.Set(traceHeader, trace)
	out := httptest.NewRecorder()
	Admin(a, func() {}, "foreground").ServeHTTP(out, req)
	if out.Header().Get(traceHeader) != trace || out.Code != http.StatusBadRequest {
		t.Fatal("trace/status lost")
	}
	text := logs.String()
	if !strings.Contains(text, trace) || !strings.Contains(text, "admin.failed") || strings.Contains(text, "CANARY") || strings.Contains(text, s.AdminToken) {
		t.Fatal("HTTP evidence lacks trace or includes payload")
	}
	panicHandler := instrument("test", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("PANIC_VALUE_CANARY") }))
	panicOutput := httptest.NewRecorder()
	panicHandler.ServeHTTP(panicOutput, httptest.NewRequest("GET", "/", nil))
	if panicOutput.Code != 500 || strings.Contains(logs.String(), "PANIC_VALUE_CANARY") || !strings.Contains(logs.String(), "http.panic") {
		t.Fatal("panic evidence unsafe or missing")
	}
}
