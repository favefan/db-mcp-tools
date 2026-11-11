package database

import (
	"encoding/json"
	"errors"
	"strings"
)

// Only structural plan estimates leave the executor. Relation/index names,
// filters, constants, expressions and extension-specific fields are omitted.
func safePlan(v any) (any, error) {
	var raw []byte
	switch x := v.(type) {
	case []byte:
		raw = x
	case string:
		raw = []byte(x)
	default:
		return nil, errors.New("unsupported Explain result")
	}
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if json.Unmarshal(raw, &plans) != nil || len(plans) != 1 {
		return nil, errors.New("invalid Explain result")
	}
	var clean func(map[string]any, int) (map[string]any, error)
	clean = func(node map[string]any, depth int) (map[string]any, error) {
		if depth > 32 {
			return nil, errors.New("Explain plan is too deep")
		}
		out := map[string]any{}
		if name, ok := node["Node Type"].(string); ok {
			out["node_type"] = strings.SplitN(name, " (", 2)[0]
		}
		for source, target := range map[string]string{"Startup Cost": "startup_cost", "Total Cost": "total_cost", "Plan Rows": "estimated_rows", "Plan Width": "estimated_width"} {
			if value, ok := node[source].(float64); ok {
				out[target] = value
			}
		}
		if children, ok := node["Plans"].([]any); ok {
			result := []any{}
			for _, child := range children {
				m, ok := child.(map[string]any)
				if !ok {
					return nil, errors.New("invalid Explain node")
				}
				next, err := clean(m, depth+1)
				if err != nil {
					return nil, err
				}
				result = append(result, next)
			}
			out["plans"] = result
		}
		return out, nil
	}
	out, err := clean(plans[0].Plan, 0)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(out)
	return string(b), err
}
