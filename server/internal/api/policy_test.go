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

func TestDefaultPoliciesStayEmptyAndTemplateMasksSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	a := app.New(s)
	defer a.Close()
	h := Admin(a, func() {}, "foreground")
	for _, path := range []string{"default-policy", "default-policy?kind=binding"} {
		req := httptest.NewRequest("GET", "/admin/v1/"+path, nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+s.AdminToken)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		var p model.Policy
		if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &p) != nil || p.Masks == nil || len(p.Masks) != 0 || strings.Contains(out.Body.String(), "pii") {
			t.Fatalf("default policy still contains presets: %d %s", out.Code, out.Body.String())
		}
	}
	p := model.DefaultPolicy()
	p.Masks = []model.MaskRule{{ID: "user-rule", Target: "notes", Mode: "fixed", Value: "hidden"}}
	template, err := s.SaveTemplate(model.Template{Name: "user template", Policy: p})
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range []model.Policy{model.DefaultPolicy(), template.Policy} {
		if _, err = s.SaveInstance(model.Instance{Name: "policy fixture", Policy: policy}); err != nil {
			t.Fatal(err)
		}
	}
	// Old exports remain readable, but the retired fields never reappear in API output.
	encoded, _ := json.Marshal(p)
	legacy := strings.TrimSuffix(string(encoded), "}") + `,"detect_pii":true,"pii_detectors":["email"]}`
	if err = json.Unmarshal([]byte(legacy), &p); err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(p)
	if strings.Contains(string(encoded), "pii") || len(p.Masks) != 1 {
		t.Fatal("legacy read lost explicit rules or retained content detection")
	}
	if json.Unmarshal([]byte(`{"unknown_policy_option":true}`), &p) == nil {
		t.Fatal("unknown policy fields accepted")
	}
	a.Close()
	s.Close()
	s, err = storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	instances, err := s.Instances()
	if err != nil || len(instances) != 2 {
		t.Fatal("policies not retained", err)
	}
	counts := map[int]int{}
	for _, instance := range instances {
		counts[len(instance.Policy.Masks)]++
	}
	templates, err := s.Templates()
	if err != nil || counts[0] != 1 || counts[1] != 1 || len(templates) != 1 || len(templates[0].Policy.Masks) != 1 || templates[0].Policy.Masks[0] != template.Policy.Masks[0] {
		t.Fatal("empty policy reseeded or template rules changed", err)
	}
}
