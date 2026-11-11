package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/database"
	"dbmcp.local/server/internal/diagnostics"
	"dbmcp.local/server/internal/mask"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/sqlguard"
	"dbmcp.local/server/internal/storage"
)

func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	name := "invalid_request"
	switch {
	case errors.Is(err, storage.ErrConflict):
		code = http.StatusConflict
		name = "conflict"
	case errors.Is(err, storage.ErrDenied), errors.Is(err, storage.ErrRegistrationPending):
		code = http.StatusForbidden
		name = "access_denied"
	case errors.Is(err, sql.ErrNoRows):
		code = http.StatusNotFound
		name = "not_found"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": name, "message": err.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("invalid JSON request")
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request must contain one JSON value")
	}
	return nil
}

type endpoint func(http.ResponseWriter, *http.Request) error

func route(m *http.ServeMux, pattern string, f endpoint) {
	m.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			diagnostics.Event(r.Context(), slog.LevelWarn, "admin.failed", "route", pattern, "error", diagnostics.ErrorInfo(err))
			fail(w, err)
		}
	})
}

func Admin(a *app.App, stop func(), mode string) http.Handler {
	m := http.NewServeMux()
	s := a.Store
	registrationRoutes(m, a)
	route(m, "GET /admin/v1/diagnostics", func(w http.ResponseWriter, r *http.Request) error {
		write(w, diagnosticSnapshot(a, mode))
		return nil
	})
	route(m, "GET /admin/v1/health", func(w http.ResponseWriter, r *http.Request) error {
		write(w, map[string]any{"server_id": s.ServerID, "data_dir": s.Dir, "version": model.Version, "api_version": model.APIVersion, "mode": mode, "drivers": map[string]bool{"postgres": true, "mysql": true, "sqlserver": true, "dm": database.DMAvailable}})
		return nil
	})
	route(m, "GET /admin/v1/default-policy", func(w http.ResponseWriter, r *http.Request) error {
		if r.URL.Query().Get("kind") == "binding" {
			write(w, model.DefaultBindingPolicy())
		} else {
			write(w, model.DefaultPolicy())
		}
		return nil
	})
	route(m, "GET /admin/v1/instances", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Instances()
		if err == nil {
			write(w, v)
		}
		return err
	})
	saveInstance := func(w http.ResponseWriter, r *http.Request) error {
		var v model.Instance
		if err := decode(w, r, &v); err != nil {
			return err
		}
		if id := r.PathValue("id"); id != "" {
			if v.ID != "" && v.ID != id {
				return errors.New("instance ID mismatch")
			}
			v.ID = id
		}
		if err := database.Validate(v); err != nil {
			return err
		}
		if err := app.ValidatePolicy(v.Policy); err != nil {
			return err
		}
		out, err := s.SaveInstance(v)
		if err != nil {
			return err
		}
		a.DB.Invalidate(out.ID)
		a.Changed()
		_ = s.Audit(model.Audit{InstanceID: out.ID, Action: "instance.save", Decision: "saved"})
		write(w, out)
		return nil
	}
	route(m, "POST /admin/v1/instances", saveInstance)
	route(m, "PUT /admin/v1/instances/{id}", saveInstance)
	route(m, "GET /admin/v1/instances/{id}", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Instance(r.PathValue("id"), false)
		if err == nil {
			write(w, v)
		}
		return err
	})
	withPassword := func(v model.Instance) (model.Instance, error) {
		if v.ID != "" && v.Connection.Password == "" {
			old, err := s.Instance(v.ID, true)
			if err != nil {
				return v, err
			}
			v.Connection.Password = old.Connection.Password
		}
		return v, nil
	}
	readConnection := func(w http.ResponseWriter, r *http.Request) (model.Instance, error) {
		var v model.Instance
		if err := decode(w, r, &v); err != nil {
			return v, err
		}
		return withPassword(v)
	}
	route(m, "POST /admin/v1/instances/databases", func(w http.ResponseWriter, r *http.Request) error {
		v, err := readConnection(w, r)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		names, err := a.DB.Discover(ctx, v)
		if err != nil {
			return err
		}
		write(w, names)
		return nil
	})
	route(m, "POST /admin/v1/instances/schema", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Instance model.Instance `json:"instance"`
			Database string         `json:"database"`
		}
		if err := decode(w, r, &input); err != nil {
			return err
		}
		v, err := withPassword(input.Instance)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		catalog, err := database.Inspect(ctx, v, input.Database)
		if err != nil {
			return err
		}
		catalog.Columns = app.VisibleColumns(catalog.Columns)
		catalog.Schemas = slices.DeleteFunc(catalog.Schemas, sqlguard.SystemSchema)
		write(w, catalog)
		return nil
	})
	route(m, "GET /admin/v1/instances/{id}/databases", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Instance(r.PathValue("id"), true)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		names, err := a.DB.Databases(ctx, v)
		if err != nil {
			return err
		}
		write(w, slices.DeleteFunc(names, func(name string) bool { return !v.Policy.AllowsDatabase(name) }))
		return nil
	})
	route(m, "POST /admin/v1/instances/test", func(w http.ResponseWriter, r *http.Request) error {
		v, err := readConnection(w, r)
		if err != nil {
			return err
		}
		if err := database.ValidateConnection(v); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := a.DB.Test(ctx, v); err != nil {
			return err
		}
		write(w, map[string]bool{"connected": true})
		return nil
	})
	route(m, "GET /admin/v1/instances/{id}/schema", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Instance(r.PathValue("id"), true)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		v, err = a.DB.Select(ctx, v, r.URL.Query().Get("database"))
		if err != nil {
			return err
		}
		if !v.Policy.AllowsDatabase(v.Connection.Database) {
			return storage.ErrDenied
		}
		catalog, err := a.DB.Describe(ctx, v)
		if err != nil {
			return err
		}
		catalog.Columns = app.VisibleColumns(catalog.Columns, v.Policy)
		catalog.Schemas = slices.DeleteFunc(catalog.Schemas, func(schema string) bool {
			return sqlguard.SystemSchema(schema) || (!v.Policy.AllowsColumn(v.Connection.Database, schema, "", "") && !slices.ContainsFunc(catalog.Columns, func(c model.Column) bool { return c.Schema == schema }))
		})
		write(w, catalog)
		return nil
	})
	route(m, "GET /admin/v1/templates", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Templates()
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "POST /admin/v1/templates", func(w http.ResponseWriter, r *http.Request) error {
		var v model.Template
		if err := decode(w, r, &v); err != nil {
			return err
		}
		if v.Name == "" {
			return errors.New("name required")
		}
		if err := app.ValidatePolicy(v.Policy); err != nil {
			return err
		}
		out, err := s.SaveTemplate(v)
		if err == nil {
			write(w, out)
			a.Changed()
		}
		return err
	})
	route(m, "GET /admin/v1/entities", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Entities()
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "GET /admin/v1/instances/{id}/entities", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.InstanceEntities(r.PathValue("id"))
		if err == nil {
			write(w, v)
		}
		return err
	})
	saveEntity := func(w http.ResponseWriter, r *http.Request) error {
		var v model.Entity
		if err := decode(w, r, &v); err != nil {
			return err
		}
		if id := r.PathValue("id"); id != "" {
			if v.ID != "" && v.ID != id {
				return errors.New("entity ID mismatch")
			}
			v.ID = id
		}
		if v.Name == "" || len(v.Name) > 200 {
			return errors.New("entity name required")
		}
		out, err := s.SaveEntity(v)
		if err == nil {
			write(w, out)
			a.Changed()
			_ = s.Audit(model.Audit{EntityID: out.ID, Action: "entity.save", Decision: "saved"})
		}
		return err
	}
	route(m, "POST /admin/v1/entities", saveEntity)
	route(m, "PUT /admin/v1/entities/{id}", saveEntity)
	route(m, "GET /admin/v1/entities/{id}/bindings", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Bindings(r.PathValue("id"))
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "GET /admin/v1/entities/{id}/effective/{instance}", func(w http.ResponseWriter, r *http.Request) error {
		v, err := a.Effective(r.PathValue("id"), r.PathValue("instance"))
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "PUT /admin/v1/entities/{id}/bindings/{instance}", func(w http.ResponseWriter, r *http.Request) error {
		var v model.Binding
		if err := decode(w, r, &v); err != nil {
			return err
		}
		v.EntityID = r.PathValue("id")
		v.InstanceID = r.PathValue("instance")
		if err := app.ValidatePolicy(v.Policy); err != nil {
			return err
		}
		if err := app.ValidateApprovalRules(v.ApprovalRules); err != nil {
			return err
		}
		out, err := s.SaveBinding(v)
		if err == nil {
			write(w, out)
			a.Changed()
			_ = s.Audit(model.Audit{EntityID: out.EntityID, InstanceID: out.InstanceID, Action: "binding.save", Decision: "saved"})
		}
		return err
	})
	route(m, "GET /admin/v1/entities/{id}/credentials", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Credentials(r.PathValue("id"))
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "POST /admin/v1/entities/{id}/credentials", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			ExpiresAt string `json:"expires_at"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		v, token, err := s.Issue(r.PathValue("id"), in.ExpiresAt)
		if err == nil {
			write(w, map[string]any{"credential": v, "token": token})
			_ = s.Audit(model.Audit{EntityID: v.EntityID, Action: "credential.issue", Decision: "issued"})
		}
		return err
	})
	route(m, "POST /admin/v1/credentials/{id}/revoke", func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Revoke(r.PathValue("id")); err != nil {
			return err
		}
		a.Changed()
		_ = s.Audit(model.Audit{Action: "credential.revoke", Decision: "revoked"})
		write(w, map[string]bool{"revoked": true})
		return nil
	})
	route(m, "POST /admin/v1/credentials/{id}/rotate", func(w http.ResponseWriter, r *http.Request) error {
		v, token, err := s.Rotate(r.PathValue("id"))
		if err == nil {
			a.Changed()
			write(w, map[string]any{"credential": v, "token": token})
			_ = s.Audit(model.Audit{EntityID: v.EntityID, Action: "credential.rotate", Decision: "rotated"})
		}
		return err
	})
	route(m, "GET /admin/v1/approvals", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Requests()
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "GET /admin/v1/approvals/{id}", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Request(r.PathValue("id"), true)
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "POST /admin/v1/approvals/{id}/{decision}", func(w http.ResponseWriter, r *http.Request) error {
		if err := a.Decide(r.PathValue("id"), r.PathValue("decision")); err != nil {
			return err
		}
		write(w, map[string]bool{"recorded": true})
		return nil
	})
	route(m, "GET /admin/v1/audit-events", func(w http.ResponseWriter, r *http.Request) error {
		q := r.URL.Query()
		before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
		from, to := q.Get("from"), q.Get("to")
		for _, s := range []*string{&from, &to} {
			if *s != "" {
				v, err := time.Parse(time.RFC3339, *s)
				if err != nil {
					return errors.New("invalid audit timestamp")
				}
				*s = v.UTC().Format(time.RFC3339)
			}
		}
		v, err := s.AuditsRange(q.Get("entity_id"), q.Get("instance_id"), q.Get("decision"), before, from, to)
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "GET /admin/v1/audit-events/{id}", func(w http.ResponseWriter, r *http.Request) error {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			return errors.New("invalid audit event ID")
		}
		v, err := s.AuditDetail(id)
		if err == nil {
			w.Header().Set("Cache-Control", "no-store")
			write(w, v)
		}
		return err
	})
	route(m, "POST /admin/v1/mask-preview", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Rules  []model.MaskRule `json:"rules"`
			Source model.Source     `json:"source"`
			Value  any              `json:"value"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		if err := mask.Validate(in.Rules); err != nil {
			return err
		}
		if in.Source.Column == "" {
			return errors.New("preview source column required")
		}
		out := in.Value
		for _, rule := range in.Rules {
			if mask.Matches(rule, in.Source) {
				out = mask.Apply(rule, out)
			}
		}
		write(w, map[string]any{"value": out})
		return nil
	})
	route(m, "POST /admin/v1/simulate", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			EntityID     string      `json:"entity_id"`
			CredentialID string      `json:"credential_id"`
			Source       string      `json:"source"`
			Query        model.Query `json:"query"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		v, err := a.Simulate(r.Context(), model.Identity{EntityID: in.EntityID, CredentialID: in.CredentialID, Source: in.Source}, in.Query)
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "POST /admin/v1/backups", func(w http.ResponseWriter, r *http.Request) error {
		p, err := s.Backup(r.Context())
		if err == nil {
			write(w, map[string]string{"path": p})
		}
		return err
	})
	route(m, "GET /admin/v1/backups", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Backups()
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "GET /admin/v1/configuration", func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Export(r.Context())
		if err == nil {
			write(w, v)
		}
		return err
	})
	route(m, "POST /admin/v1/configuration/preview", func(w http.ResponseWriter, r *http.Request) error {
		var v model.Configuration
		if err := decode(w, r, &v); err != nil {
			return err
		}
		plan, err := a.ImportPlan(v)
		if err == nil {
			write(w, plan)
		}
		return err
	})
	route(m, "POST /admin/v1/configuration/import", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Configuration model.Configuration `json:"configuration"`
			Digest        string              `json:"digest"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		if err := a.Import(r.Context(), in.Configuration, in.Digest); err != nil {
			return err
		}
		write(w, map[string]bool{"imported": true})
		return nil
	})
	route(m, "POST /admin/v1/runtime/stop", func(w http.ResponseWriter, r *http.Request) error {
		if mode != "background" {
			return errors.New("this process is managed by its terminal or service manager")
		}
		if r.Header.Get("X-DBMCP-Server-ID") != s.ServerID {
			return storage.ErrDenied
		}
		write(w, map[string]bool{"stopping": true})
		go stop()
		return nil
	})
	m.HandleFunc("GET /admin/v1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		f, ok := w.(http.Flusher)
		if !ok {
			return
		}
		changes, unsubscribe := a.Subscribe()
		defer unsubscribe()
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		_, _ = fmt.Fprint(w, "event: changed\ndata: {}\n\n")
		f.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-changes:
				_, _ = fmt.Fprint(w, "event: changed\ndata: {}\n\n")
				f.Flush()
			case <-ticker.C:
				_, _ = fmt.Fprint(w, ": heartbeat\n\n")
				f.Flush()
				continue
			}
		}
	})
	return instrument("admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/health" && r.Method == "GET" {
			write(w, map[string]string{"status": "ok"})
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.AdminToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			http.Error(w, "browser access is disabled; use the desktop proxy", http.StatusForbidden)
			return
		}
		if expected := r.Header.Get("X-DBMCP-Server-ID"); expected != "" && expected != s.ServerID {
			fail(w, storage.ErrDenied)
			return
		}
		m.ServeHTTP(w, r)
	}))
}

func source(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}
