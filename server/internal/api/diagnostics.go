package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"time"

	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/database"
	"dbmcp.local/server/internal/diagnostics"
	"dbmcp.local/server/internal/model"
)

const traceHeader = "X-DBMCP-Trace-ID"

var validTrace = regexp.MustCompile(`^[a-f0-9]{32}$`)

type responseStatus struct {
	http.ResponseWriter
	status int
}

func (w *responseStatus) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *responseStatus) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *responseStatus) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseStatus) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func instrument(component string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace := r.Header.Get(traceHeader)
		if !validTrace.MatchString(trace) {
			trace = model.ID()
		}
		r = r.WithContext(diagnostics.WithTrace(r.Context(), trace))
		w.Header().Set(traceHeader, trace)
		out := &responseStatus{ResponseWriter: w}
		start := time.Now()
		defer func() {
			if value := recover(); value != nil {
				if value == http.ErrAbortHandler {
					panic(value)
				}
				diagnostics.Event(r.Context(), slog.LevelError, "http.panic", "component", component, "stack", string(debug.Stack()))
				if out.status == 0 {
					http.Error(out, "internal error; provide trace ID "+trace, 500)
				}
			}
			status := out.status
			if status == 0 {
				status = 200
			}
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			level := slog.LevelInfo
			if status >= 400 {
				level = slog.LevelWarn
			}
			diagnostics.Event(r.Context(), level, "http.complete", "component", component, "route", route, "status", status, "duration_ms", time.Since(start).Milliseconds())
		}()
		handler.ServeHTTP(out, r)
	})
}

func diagnosticSnapshot(a *app.App, mode string) map[string]any {
	s := a.Store
	out := map[string]any{"format": "dbmcp-diagnostics/v1", "created_at": time.Now().UTC().Format(time.RFC3339Nano), "build": diagnostics.Build(), "server_id": s.ServerID, "mode": mode, "data_dir": s.Dir, "logs": diagnostics.Collect(filepath.Join(s.Dir, "logs")), "process_logs": diagnostics.Collect(diagnostics.UserDir()), "issues": []string{}}
	issues := []string{}
	counts := map[string]int{}
	for _, table := range []string{"instances", "entities", "bindings", "registrations", "requests"} {
		var count int
		if err := s.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			issues = append(issues, "cannot count "+table)
		} else {
			counts[table] = count
		}
	}
	out["state_counts"] = counts
	instances, err := s.Instances()
	if err != nil {
		issues = append(issues, "cannot read instance diagnostics")
	}
	if len(instances) > 100 {
		instances = instances[:100]
		issues = append(issues, "instance configuration capped at 100")
	}
	settings := []map[string]any{}
	for _, i := range instances {
		settings = append(settings, map[string]any{"id": i.ID, "driver": i.Driver, "enabled": i.Enabled, "revision": i.Revision, "host": i.Connection.Host, "port": i.Connection.Port, "database_mode": database.DatabaseMode(i.Connection), "database": i.Connection.Database, "databases": i.Connection.Databases, "tls": i.Connection.TLS, "policy": safePolicy(i.Policy)})
	}
	out["instances"] = settings
	entities, err := s.Entities()
	if err != nil {
		issues = append(issues, "cannot read entity diagnostics")
	}
	if len(entities) > 100 {
		entities = entities[:100]
		issues = append(issues, "entity configuration capped at 100")
	}
	identity := []map[string]any{}
	for _, e := range entities {
		credentials, credentialErr := s.Credentials(e.ID)
		credentialState := []map[string]any{}
		if credentialErr != nil {
			issues = append(issues, "cannot read credential status")
		} else {
			for _, c := range credentials {
				credentialState = append(credentialState, map[string]any{"id": c.ID, "revoked": c.Revoked, "expires_at": c.ExpiresAt})
			}
		}
		bindings, err := s.Bindings(e.ID)
		if err != nil {
			issues = append(issues, "cannot read binding diagnostics")
			continue
		}
		perms := []map[string]any{}
		for _, b := range bindings {
			perms = append(perms, map[string]any{"instance_id": b.InstanceID, "revision": b.Revision, "policy": safePolicy(b.Policy), "approval_rules": b.ApprovalRules})
		}
		identity = append(identity, map[string]any{"id": e.ID, "enabled": e.Enabled, "revision": e.Revision, "bindings": perms, "credential_status": credentialState})
	}
	out["entities"] = identity
	if registrations, err := s.Registrations(); err != nil {
		issues = append(issues, "cannot read registration status")
	} else {
		states := []map[string]any{}
		for _, r := range registrations {
			if len(states) == 100 {
				issues = append(issues, "registration status capped at 100")
				break
			}
			states = append(states, map[string]any{"id": r.ID, "entity_id": r.EntityID, "status": r.Status, "requested_at": r.RequestedAt, "expires_at": r.ExpiresAt})
		}
		out["registrations"] = states
	}
	audits := []model.Audit{}
	before := int64(0)
	for len(audits) < 1000 {
		page, err := s.Audits("", "", "", before)
		if err != nil {
			issues = append(issues, "cannot read audit diagnostics")
			break
		}
		if len(page) == 0 {
			break
		}
		for _, v := range page {
			v.Detail = ""
			audits = append(audits, v)
			if len(audits) == 1000 {
				break
			}
		}
		before = page[len(page)-1].ID
		if len(page) < 200 {
			break
		}
	}
	out["audit_events"] = audits
	out["audit_limit"] = 1000
	out["audit_may_be_truncated"] = len(audits) == 1000
	out["issues"] = issues
	return out
}
func safePolicy(policy model.Policy) map[string]any {
	b, _ := json.Marshal(policy)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	// Replacement values are arbitrary user data; matching and mode are enough
	// to identify which rule ran without exporting their configured contents.
	if masks, ok := out["masks"].([]any); ok {
		for _, m := range masks {
			if fields, ok := m.(map[string]any); ok {
				delete(fields, "replacement")
				delete(fields, "value")
			}
		}
	}
	return out
}
