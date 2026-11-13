package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/diagnostics"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type identityKey struct{}
type empty struct{}
type instanceInput struct {
	InstanceID string `json:"instance_id"`
	Database   string `json:"database,omitempty"`
}
type tableInput struct {
	InstanceID string `json:"instance_id"`
	Database   string `json:"database,omitempty"`
	Schema     string `json:"schema"`
	Table      string `json:"table"`
}
type tablesInput struct {
	InstanceID string `json:"instance_id"`
	Database   string `json:"database,omitempty"`
	Schema     string `json:"schema,omitempty"`
}
type requestInput struct {
	RequestID string `json:"request_id"`
}

func MCP(a *app.App, resolvers ...SourceResolver) http.Handler {
	resolve := SourceResolver(source)
	if len(resolvers) > 0 {
		resolve = resolvers[0]
	}
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		id, ok := r.Context().Value(identityKey{}).(model.Identity)
		if !ok {
			return nil
		}
		return mcpServer(a, id)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 2 << 20, PropagateRequestCancellation: true})
	return instrument("mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		clientIP := resolve(r)
		if clientIP == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id, err := a.Store.Authenticate(token, clientIP)
		if errors.Is(err, storage.ErrRegistrationPending) {
			diagnostics.Event(r.Context(), slog.LevelInfo, "registration.pending", "entity_id", id.EntityID)
			_ = a.Store.Observe(id.EntityID, id.Source, r.Header.Get("User-Agent"))
			a.Changed()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "15")
			w.WriteHeader(http.StatusForbidden)
			write(w, map[string]string{"code": "entity_pending", "message": "Agent 已登记，等待管理员在待审批中确认实例和权限；批准后重新连接 MCP。", "entity_id": id.EntityID})
			return
		}
		if err != nil || clientIP == "" {
			diagnostics.Event(r.Context(), slog.LevelWarn, "mcp.authentication_denied", "error", diagnostics.ErrorInfo(err))
			_ = a.Store.Audit(model.Audit{Action: "authenticate", Decision: "denied", Detail: "source=" + clientIP})
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = a.Store.Observe(id.EntityID, id.Source, r.Header.Get("User-Agent"))
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	}))
}
func mcpServer(a *app.App, id model.Identity) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "db-mcp-tools", Version: model.Version}, &mcp.ServerOptions{Instructions: "Database access is scoped to this entity. Call list_databases for an instance and pass its exact database name to schema and query tools; database is required for multi-database instances. Each request targets one database; cross-database SQL and USE are unsupported. For requests that may require approval, include agent_reason: an explanation of the purpose and action in at most 100 characters. Missing explanation does not block a request. Writes may return pending. Ask the administrator to approve, then call approval_status and resume_request with the same request_id. Never replace an approved SQL payload. Always supply a unique client_request_id per operation and reuse it on retries."})
	mcp.AddTool(s, &mcp.Tool{Name: "list_instances", Description: "List the database instances bound to this identity."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
		v, err := a.VisibleInstances(id)
		if err != nil {
			return nil, nil, err
		}
		out := []map[string]string{}
		for _, i := range v {
			out = append(out, map[string]string{"id": i.ID, "name": i.Name, "driver": i.Driver})
		}
		return nil, map[string]any{"instances": out}, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "list_databases", Description: "List databases within the bound instance's configured scope and account access."}, func(ctx context.Context, _ *mcp.CallToolRequest, in instanceInput) (*mcp.CallToolResult, any, error) {
		names, err := a.Databases(ctx, id, in.InstanceID)
		return nil, map[string]any{"databases": names}, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "list_schemas", Description: "List schemas containing visible tables and columns."}, func(ctx context.Context, _ *mcp.CallToolRequest, in instanceInput) (*mcp.CallToolResult, any, error) {
		cols, err := a.Catalog(ctx, id, in.InstanceID, in.Database)
		if err != nil {
			return nil, nil, err
		}
		out := []string{}
		for _, c := range cols {
			if !slices.Contains(out, c.Schema) {
				out = append(out, c.Schema)
			}
		}
		return nil, map[string]any{"schemas": out}, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "list_tables", Description: "List authorized tables, optionally restricted to a schema."}, func(ctx context.Context, _ *mcp.CallToolRequest, in tablesInput) (*mcp.CallToolResult, any, error) {
		cols, err := a.Catalog(ctx, id, in.InstanceID, in.Database)
		if err != nil {
			return nil, nil, err
		}
		out := []string{}
		for _, c := range cols {
			if in.Schema != "" && in.Schema != c.Schema {
				continue
			}
			name := c.Schema + "." + c.Table
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
		return nil, map[string]any{"tables": out}, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "describe_table", Description: "Describe authorized columns without reading sample values."}, func(ctx context.Context, _ *mcp.CallToolRequest, in tableInput) (*mcp.CallToolResult, any, error) {
		cols, err := a.Catalog(ctx, id, in.InstanceID, in.Database)
		if err != nil {
			return nil, nil, err
		}
		out := []model.Column{}
		for _, c := range cols {
			if c.Schema == in.Schema && c.Table == in.Table {
				out = append(out, c)
			}
		}
		return nil, map[string]any{"columns": out}, nil
	})
	for _, name := range []string{"db_query", "db_execute", "db_explain"} {
		mcp.AddTool(s, &mcp.Tool{Name: name, Description: map[string]string{"db_query": "Execute a single supported SELECT with mandatory authorization, masking and limits.", "db_execute": "Request a single authorized INSERT, UPDATE or DELETE. Approval may be required; repeated client_request_id never executes twice.", "db_explain": "Obtain a non-executing PostgreSQL SELECT plan. Only structural estimates are returned; authorization and masks still apply."}[name] + " Include agent_reason (at most 100 characters) explaining purpose and action when approval may be needed; omission is non-blocking."}, func(ctx context.Context, _ *mcp.CallToolRequest, in model.Query) (*mcp.CallToolResult, any, error) {
			in.Tool = name
			v, err := a.Query(ctx, id, in)
			return nil, v, err
		})
	}
	mcp.AddTool(s, &mcp.Tool{Name: "approval_status", Description: "Read the state/result of a request belonging to this credential."}, func(ctx context.Context, _ *mcp.CallToolRequest, in requestInput) (*mcp.CallToolResult, any, error) {
		v, err := a.Status(id, in.RequestID)
		return nil, v, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "resume_request", Description: "Resume the original approved request. Current permissions are checked again. Concurrent retries claim at most one execution."}, func(ctx context.Context, _ *mcp.CallToolRequest, in requestInput) (*mcp.CallToolResult, any, error) {
		v, err := a.Resume(ctx, id, in.RequestID)
		return nil, v, err
	})
	return s
}
