// Command contract generates the OpenAPI schemas directly from server models.
package main

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"

	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
	"github.com/google/jsonschema-go/jsonschema"
)

func main() {
	schemas := map[string]any{}
	for _, v := range []any{model.Registration{}, model.RegistrationApproval{}, model.Connection{}, model.MaskRule{}, model.Policy{}, model.Instance{}, model.Template{}, model.Entity{}, model.Binding{}, model.Effective{}, model.ApprovalRule{}, model.Configuration{}, model.Credential{}, model.Column{}, model.Catalog{}, model.Source{}, model.Output{}, model.Statement{}, model.Query{}, model.Result{}, model.Request{}, model.Audit{}, model.RequestDetail{}, model.ExecutionSummary{}, model.MaskMatch{}, model.AuditDetail{}, storage.BackupInfo{}} {
		s, err := jsonschema.ForType(reflect.TypeOf(v), nil)
		if err != nil {
			panic(err)
		}
		schemas[reflect.TypeOf(v).Name()] = s
	}
	paths := routes(schemas)
	v := map[string]any{"openapi": "3.1.0", "info": map[string]string{"title": "DB MCP Tools Admin API", "version": model.Version}, "security": []any{map[string]any{"adminBearer": []any{}}}, "paths": paths, "components": map[string]any{"schemas": schemas, "securitySchemes": map[string]any{"adminBearer": map[string]string{"type": "http", "scheme": "bearer"}}}}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	b = append(b, '\n')
	path := "../docs/openapi.json"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	if err = os.WriteFile(path, b, 0644); err != nil {
		panic(err)
	}
}

func routes(schemas map[string]any) map[string]any {
	ref := func(name string) any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	array := func(item any) any { return map[string]any{"type": "array", "items": item} }
	scalar := func(kind string) any { return map[string]any{"type": kind} }
	object := func(props map[string]any) any {
		required := []string{}
		for key := range props {
			required = append(required, key)
		}
		slices.Sort(required)
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	str, boolean, integer := scalar("string"), scalar("boolean"), scalar("integer")
	issued := object(map[string]any{"credential": ref("Credential"), "token": str})
	schemas["Error"] = object(map[string]any{"code": str, "message": str})
	paths := map[string]any{}
	for _, p := range []struct {
		path, method      string
		response, request any
		query             []string
	}{
		{"health", "get", object(map[string]any{"server_id": str, "instance_id": str, "data_dir": str, "version": str, "api_version": integer, "mode": str, "drivers": map[string]any{"type": "object", "additionalProperties": boolean}}), nil, nil},
		{"diagnostics", "get", map[string]any{"type": "object", "description": "Bounded diagnostic snapshot, operational logs and audit summaries; excludes credentials, SQL and row values."}, nil, nil},
		{"default-policy", "get", ref("Policy"), nil, []string{"kind"}},
		{"instances", "get", array(ref("Instance")), nil, nil},
		{"instances", "post", ref("Instance"), ref("Instance"), nil},
		{"instances/{id}", "get", ref("Instance"), nil, nil},
		{"instances/{id}", "put", ref("Instance"), ref("Instance"), nil},
		{"instances/test", "post", object(map[string]any{"connected": boolean}), ref("Instance"), nil},
		{"instances/databases", "post", array(str), ref("Instance"), nil},
		{"instances/schema", "post", ref("Catalog"), object(map[string]any{"instance": ref("Instance"), "database": str}), nil},
		{"instances/{id}/databases", "get", array(str), nil, nil},
		{"instances/{id}/schema", "get", ref("Catalog"), nil, []string{"database"}},
		{"instances/{id}/entities", "get", array(ref("Entity")), nil, nil},
		{"templates", "get", array(ref("Template")), nil, nil},
		{"templates", "post", ref("Template"), ref("Template"), nil},
		{"entities", "get", array(ref("Entity")), nil, nil},
		{"registrations", "get", array(ref("Registration")), nil, nil},
		{"registrations", "post", object(map[string]any{"registration": ref("Registration"), "token": str}), map[string]any{"type": "object", "properties": map[string]any{"name": str, "expires_at": str}, "required": []string{"name"}}, nil},
		{"registrations/{id}/approve", "post", ref("Registration"), ref("RegistrationApproval"), nil},
		{"registrations/{id}/deny", "post", object(map[string]any{"recorded": boolean}), nil, nil},
		{"registrations/{id}/revoke", "post", object(map[string]any{"recorded": boolean}), nil, nil},
		{"entities", "post", ref("Entity"), ref("Entity"), nil},
		{"entities/{id}", "put", ref("Entity"), ref("Entity"), nil},
		{"entities/{id}/bindings", "get", array(ref("Binding")), nil, nil},
		{"entities/{id}/bindings/{instance}", "put", ref("Binding"), ref("Binding"), nil},
		{"entities/{id}/effective/{instance}", "get", ref("Effective"), nil, nil},
		{"entities/{id}/credentials", "get", array(ref("Credential")), nil, nil},
		{"entities/{id}/credentials", "post", issued, map[string]any{"type": "object", "properties": map[string]any{"expires_at": str}}, nil},
		{"credentials/{id}/revoke", "post", object(map[string]any{"revoked": boolean}), nil, nil},
		{"credentials/{id}/rotate", "post", issued, nil, nil},
		{"approvals", "get", array(ref("Request")), nil, nil},
		{"approvals/{id}", "get", ref("Request"), nil, nil},
		{"approvals/{id}/{decision}", "post", object(map[string]any{"recorded": boolean}), nil, nil},
		{"audit-events", "get", array(ref("Audit")), nil, []string{"entity_id", "instance_id", "decision", "before", "from", "to"}},
		{"audit-events/{id}", "get", ref("AuditDetail"), nil, nil},
		{"mask-preview", "post", object(map[string]any{"value": map[string]any{}}), object(map[string]any{"rules": array(ref("MaskRule")), "source": ref("Source"), "value": map[string]any{}}), nil},
		{"simulate", "post", object(map[string]any{"decision": str, "database": str, "statement": ref("Statement"), "base": ref("Policy"), "binding": ref("Policy")}), object(map[string]any{"entity_id": str, "credential_id": str, "source": str, "query": ref("Query")}), nil},
		{"backups", "get", array(ref("BackupInfo")), nil, nil},
		{"backups", "post", object(map[string]any{"path": str}), nil, nil},
		{"backups/{id}", "delete", object(map[string]any{"deleted": boolean}), nil, nil},
		{"configuration", "get", ref("Configuration"), nil, nil},
		{"configuration/preview", "post", object(map[string]any{"digest": str, "instances": integer, "entities": integer, "bindings": integer, "templates": integer, "behavior": str}), ref("Configuration"), nil},
		{"configuration/import", "post", object(map[string]any{"imported": boolean}), object(map[string]any{"configuration": ref("Configuration"), "digest": str}), nil},
		{"runtime/stop", "post", object(map[string]any{"stopping": boolean}), nil, nil},
		{"events", "get", str, nil, nil},
	} {
		key := "/admin/v1/" + p.path
		obj, ok := paths[key].(map[string]any)
		if !ok {
			obj = map[string]any{}
			paths[key] = obj
		}
		contentType := "application/json"
		if p.path == "events" {
			contentType = "text/event-stream"
		}
		responses := map[string]any{"200": map[string]any{"description": "Successful response", "content": map[string]any{contentType: map[string]any{"schema": p.response}}}, "401": map[string]any{"description": "Invalid administrator credential (plain text)"}}
		for _, code := range []string{"400", "403", "404", "409"} {
			responses[code] = map[string]any{"description": "Request rejected", "content": map[string]any{"application/json": map[string]any{"schema": ref("Error")}}}
		}
		op := map[string]any{"responses": responses}
		if p.request != nil {
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": p.request}}}
		}
		params := []any{}
		for _, part := range strings.Split(p.path, "/") {
			if strings.HasPrefix(part, "{") {
				name := strings.Trim(part, "{}")
				typ := str
				if name == "decision" {
					typ = map[string]any{"type": "string", "enum": []string{"approve", "deny", "remember"}}
				}
				params = append(params, map[string]any{"name": name, "in": "path", "required": true, "schema": typ})
			}
		}
		for _, name := range p.query {
			typ := str
			if name == "before" {
				typ = integer
			}
			params = append(params, map[string]any{"name": name, "in": "query", "schema": typ})
		}
		if p.path == "runtime/stop" {
			params = append(params, map[string]any{"name": "X-DBMCP-Server-ID", "in": "header", "required": true, "schema": str})
			params = append(params, map[string]any{"name": "X-DBMCP-Instance-ID", "in": "header", "required": false, "schema": str})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		obj[p.method] = op
	}
	return paths
}
