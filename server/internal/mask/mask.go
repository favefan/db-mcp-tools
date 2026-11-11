package mask

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"dbmcp.local/server/internal/model"
)

func Validate(rules []model.MaskRule) error {
	seen := map[string]bool{}
	for _, r := range rules {
		if r.ID == "" || r.Target == "" || seen[r.ID] {
			return fmt.Errorf("mask rule ID and target required; IDs must be unique")
		}
		seen[r.ID] = true
		if r.KeepStart < 0 || r.KeepEnd < 0 || r.KeepStart+r.KeepEnd > 256 {
			return fmt.Errorf("invalid partial mask")
		}
		switch r.Mode {
		case "fixed", "partial", "null", "deny":
		case "regex":
			if len(r.Pattern) == 0 || len(r.Pattern) > 4096 {
				return fmt.Errorf("regex pattern required")
			}
			if _, err := regexp.Compile(r.Pattern); err != nil {
				return fmt.Errorf("invalid mask regex: %w", err)
			}
		default:
			return fmt.Errorf("invalid mask mode")
		}
	}
	return nil
}
func Matches(r model.MaskRule, s model.Source) bool {
	return model.Match(r.Target, model.Resource(s.Schema, s.Table, s.Column)) || model.Match(r.Target, s.Column)
}
func Denied(rules []model.MaskRule, s model.Source) bool {
	for _, r := range rules {
		if r.Mode == "deny" && Matches(r, s) {
			return true
		}
	}
	return false
}
func Apply(rule model.MaskRule, v any) any {
	if v == nil {
		return nil
	}
	switch rule.Mode {
	case "null":
		return nil
	case "fixed", "deny":
		if rule.Value != "" {
			return rule.Value
		}
		return "[REDACTED]"
	case "partial":
		r := []rune(fmt.Sprint(v))
		if len(r) <= rule.KeepStart+rule.KeepEnd {
			return "[REDACTED]"
		}
		return string(r[:rule.KeepStart]) + strings.Repeat("*", min(8, len(r)-rule.KeepStart-rule.KeepEnd)) + string(r[len(r)-rule.KeepEnd:])
	case "regex":
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return "[REDACTED]"
		}
		return re.ReplaceAllString(fmt.Sprint(v), rule.Value)
	}
	return "[REDACTED]"
}
func Value(out model.Output, v any, base, extra model.Policy) any {
	v, _ = ValueWithMatches(out, v, base, extra)
	return v
}

func ValueWithMatches(out model.Output, v any, base, extra model.Policy) (any, []model.MaskMatch) {
	matches := []model.MaskMatch{}
	for layer, rules := range [][]model.MaskRule{base.Masks, extra.Masks} {
		for _, rule := range rules {
			hit := false
			for _, s := range out.Sources {
				if Matches(rule, s) {
					hit = true
					break
				}
			}
			if hit {
				name := "instance"
				if layer == 1 {
					name = "binding"
				}
				matches = append(matches, model.MaskMatch{Layer: name, ID: rule.ID, Target: rule.Target, Mode: rule.Mode})
				if out.Derived {
					v = "[REDACTED]"
				} else {
					v = Apply(rule, v)
				}
			}
		}
	}
	if s, ok := v.(string); ok && !utf8.ValidString(s) {
		return "[BINARY]", matches
	}
	return v, matches
}
