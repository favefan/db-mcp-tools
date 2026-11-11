package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"dbmcp.local/server/internal/diagnostics"
	"dbmcp.local/server/internal/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	trace := diagnostics.Trace(r.Context())
	if trace == "" {
		trace = model.ID()
	}
	r.Header.Set(traceHeader, trace)
	return b.base.RoundTrip(r)
}

type BridgeConfig struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func Bridge(ctx context.Context, path string, wait time.Duration) error {
	diagnostics.Event(ctx, slog.LevelInfo, "bridge.start", "wait_ms", wait.Milliseconds())
	b, err := os.ReadFile(path)
	if err != nil {
		return errors.New("cannot read bridge credential file")
	}
	var cfg BridgeConfig
	if err = json.Unmarshal(b, &cfg); err != nil {
		return errors.New("invalid bridge credential file")
	}
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" || cfg.Token == "" {
		return errors.New("bridge URL and token required")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return errors.New("remote bridge requires HTTPS")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "dbmcp-bridge", Version: model.Version}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: cfg.URL, HTTPClient: &http.Client{Transport: bearerTransport{cfg.Token, http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		diagnostics.Event(ctx, slog.LevelError, "bridge.connect_failed", "error", diagnostics.ErrorInfo(err))
		return errors.New("cannot connect to dbmcp Server; start or reconnect the Server")
	}
	defer session.Close()
	s := mcp.NewServer(&mcp.Implementation{Name: "dbmcp-bridge", Version: model.Version}, nil)
	listed, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		diagnostics.Event(ctx, slog.LevelError, "bridge.discovery_failed", "error", diagnostics.ErrorInfo(err))
		return errors.New("cannot discover Server tools")
	}
	for _, tool := range listed.Tools {
		tool := tool
		s.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx = diagnostics.WithTrace(ctx, model.ID())
			start := time.Now()
			diagnostics.Event(ctx, slog.LevelInfo, "bridge.tool_start", "tool", tool.Name)
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: json.RawMessage(req.Params.Arguments)})
			if err != nil {
				diagnostics.Event(ctx, slog.LevelError, "bridge.tool_failed", "tool", tool.Name, "duration_ms", time.Since(start).Milliseconds(), "error", diagnostics.ErrorInfo(err))
			}
			if err != nil || wait <= 0 {
				return result, err
			}
			if tool.Name != "db_execute" && tool.Name != "db_query" && tool.Name != "db_explain" && tool.Name != "resume_request" {
				return result, nil
			}
			deadline := time.NewTimer(wait)
			defer deadline.Stop()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				encoded, _ := json.Marshal(result.StructuredContent)
				var state struct {
					Status    string `json:"status"`
					RequestID string `json:"request_id"`
				}
				_ = json.Unmarshal(encoded, &state)
				if state.Status != "pending" && state.Status != "approved" {
					diagnostics.Event(ctx, slog.LevelInfo, "bridge.tool_complete", "tool", tool.Name, "request_id", state.RequestID, "status", state.Status, "duration_ms", time.Since(start).Milliseconds())
					return result, nil
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-deadline.C:
					diagnostics.Event(ctx, slog.LevelInfo, "bridge.approval_wait_ended", "request_id", state.RequestID, "status", state.Status)
					return result, nil
				case <-ticker.C:
				}
				result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "resume_request", Arguments: map[string]string{"request_id": state.RequestID}})
				if err != nil {
					return nil, err
				}
			}
		})
	}
	return s.Run(ctx, &mcp.StdioTransport{})
}
