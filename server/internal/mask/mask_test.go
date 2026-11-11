package mask

import (
	"dbmcp.local/server/internal/model"
	"encoding/json"
	"testing"
)

func TestOnlyExplicitRulesMaskValues(t *testing.T) {
	base := model.DefaultPolicy()
	column := model.Output{Name: "notes", Sources: []model.Source{{Schema: "public", Table: "users", Column: "notes"}}}
	for _, value := range []any{"person@example.com", "13812345678", int64(13812345678), "secret"} {
		if got := Value(column, value, base, model.DefaultBindingPolicy()); got != value {
			t.Fatalf("value masked without a rule: %v => %v", value, got)
		}
	}
	legacy := `{"detect_pii":true,"pii_detectors":["email","phone"],"masks":[]}`
	if err := json.Unmarshal([]byte(legacy), &base); err != nil {
		t.Fatal(err)
	}
	if got := Value(column, "person@example.com 13812345678", base, model.Policy{}); got != "person@example.com 13812345678" {
		t.Fatal("retired detector still applied", got)
	}
	base.Masks = []model.MaskRule{{ID: "custom", Target: "notes", Mode: "regex", Pattern: `[a-z]+@example\.com`, Value: "[hidden]"}}
	if got := Value(column, "Contact person@example.com", base, model.Policy{}); got != "Contact [hidden]" {
		t.Fatal("explicit regex did not apply", got)
	}
}

func TestInstanceRulesAndDerivedValuesCannotBeRelaxed(t *testing.T) {
	base := model.DefaultPolicy()
	base.Masks = []model.MaskRule{{ID: "mail", Target: "public.users.email", Mode: "partial", KeepStart: 2, KeepEnd: 2}}
	column := model.Output{Name: "alias", Sources: []model.Source{{Schema: "public", Table: "users", Column: "email"}}}
	extra := model.Policy{}
	if got := Value(column, "person@example.com", base, extra); got == "person@example.com" {
		t.Fatal("empty binding masks disabled instance rules")
	}
	column.Derived = true
	if got := Value(column, 17, base, extra); got != "[REDACTED]" {
		t.Fatalf("derived sensitive value exposed: %v", got)
	}
	base.Masks = []model.MaskRule{{ID: "deny", Target: "email", Mode: "deny"}}
	if !Denied(base.Masks, column.Sources[0]) {
		t.Fatal("deny was treated as output-only masking")
	}
}
