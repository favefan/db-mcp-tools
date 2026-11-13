// Package app is the single identity -> policy -> approval -> execution -> mask
// -> audit entry point used by MCP, Bridge and the administrator simulator.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"dbmcp.local/server/internal/database"
	"dbmcp.local/server/internal/diagnostics"
	"dbmcp.local/server/internal/mask"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/sqlguard"
	"dbmcp.local/server/internal/storage"
)

type App struct {
	Store       *storage.Store
	DB          *database.Manager
	eventMu     sync.Mutex
	subscribers map[chan struct{}]struct{}
}

func New(s *storage.Store) *App {
	return &App{Store: s, DB: database.New(), subscribers: make(map[chan struct{}]struct{})}
}
func (a *App) Changed() {
	a.eventMu.Lock()
	defer a.eventMu.Unlock()
	for ch := range a.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (a *App) Subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	a.eventMu.Lock()
	a.subscribers[ch] = struct{}{}
	a.eventMu.Unlock()
	return ch, func() { a.eventMu.Lock(); delete(a.subscribers, ch); a.eventMu.Unlock() }
}
func (a *App) Close() { a.DB.Close() }

type prepared struct {
	instance             model.Instance
	binding              model.Binding
	info                 model.Statement
	approval             bool
	rows, bytes, timeout int
}

func effective(i model.Instance, b model.Binding) model.Effective {
	ops := []string{}
	for _, op := range i.Policy.Operations {
		if slices.Contains(b.Policy.Operations, op) {
			ops = append(ops, op)
		}
	}
	mode := "never"
	if i.Policy.Approval == "always" || b.Policy.Approval == "always" {
		mode = "always"
	} else if i.Policy.Approval == "writes" || b.Policy.Approval == "writes" {
		mode = "writes"
	}
	return model.Effective{InstancePolicy: i.Policy, Binding: b, Operations: ops, Approval: mode, MaxRows: min(i.Policy.MaxRows, b.Policy.MaxRows), MaxBytes: min(i.Policy.MaxBytes, b.Policy.MaxBytes), TimeoutSeconds: min(i.Policy.TimeoutSeconds, b.Policy.TimeoutSeconds), Concurrency: min(i.Policy.Concurrency, b.Policy.Concurrency), InstanceEnabled: i.Enabled}
}
func (a *App) Effective(entity, instance string) (model.Effective, error) {
	e, err := a.Store.Entity(entity)
	if err != nil {
		return model.Effective{}, err
	}
	i, err := a.Store.Instance(instance, false)
	if err != nil {
		return model.Effective{}, err
	}
	b, err := a.Store.Binding(entity, instance)
	if err != nil {
		return model.Effective{}, err
	}
	v := effective(i, b)
	v.EntityEnabled = e.Enabled
	return v, nil
}

func ValidatePolicy(p model.Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := mask.Validate(p.Masks); err != nil {
		return err
	}
	for _, c := range p.CIDRs {
		if _, err := netip.ParsePrefix(c); err != nil {
			return fmt.Errorf("invalid CIDR %q", c)
		}
	}
	return nil
}
func ValidateApprovalRules(rules []model.ApprovalRule) error {
	for _, r := range rules {
		if r.ID == "" || !slices.Contains([]string{"select", "insert", "update", "delete"}, r.Operation) || len(r.Tables) < 1 || len(r.Tables) > 16 {
			return errors.New("invalid remembered approval scope")
		}
		for _, t := range r.Tables {
			if t.Schema == "" || t.Table == "" || t.Column != "" || strings.ContainsAny(t.Schema+t.Table, "*?[]") {
				return errors.New("remembered approvals require explicit schema/table names")
			}
		}
	}
	return nil
}
func sourceAllowed(p model.Policy, source string) bool {
	ip, err := netip.ParseAddr(source)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	for _, v := range p.CIDRs {
		cidr, e := netip.ParsePrefix(v)
		if e == nil && cidr.Contains(ip) {
			return true
		}
	}
	return false
}
func (a *App) policies(i model.Identity, instanceID string) (model.Instance, model.Binding, error) {
	if err := a.Store.Recheck(i); err != nil {
		return model.Instance{}, model.Binding{}, err
	}
	in, err := a.Store.Instance(instanceID, true)
	if err != nil || !in.Enabled {
		return in, model.Binding{}, storage.ErrDenied
	}
	b, err := a.Store.Binding(i.EntityID, instanceID)
	if err != nil {
		return in, b, storage.ErrDenied
	}
	if len(b.Policy.Resources) == 0 || len(b.Policy.Operations) == 0 {
		return in, b, storage.ErrDenied
	}
	if !sourceAllowed(in.Policy, i.Source) || !sourceAllowed(b.Policy, i.Source) {
		return in, b, storage.ErrDenied
	}
	return in, b, nil
}
func (a *App) VisibleInstances(i model.Identity) ([]model.Instance, error) {
	if err := a.Store.Recheck(i); err != nil {
		return nil, err
	}
	all, err := a.Store.Instances()
	if err != nil {
		return nil, err
	}
	out := []model.Instance{}
	for _, v := range all {
		if _, _, err := a.policies(i, v.ID); err == nil {
			v.Connection = model.Connection{}
			out = append(out, v)
		}
	}
	return out, nil
}
func (a *App) Databases(ctx context.Context, id model.Identity, instanceID string) ([]string, error) {
	i, b, err := a.policies(id, instanceID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(min(i.Policy.TimeoutSeconds, b.Policy.TimeoutSeconds))*time.Second)
	defer cancel()
	names, err := a.DB.Databases(ctx, i)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(names, func(name string) bool { return !i.Policy.AllowsDatabase(name) || !b.Policy.AllowsDatabase(name) }), nil
}
func (a *App) Catalog(ctx context.Context, id model.Identity, instanceID string, names ...string) ([]model.Column, error) {
	i, b, err := a.policies(id, instanceID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(min(i.Policy.TimeoutSeconds, b.Policy.TimeoutSeconds))*time.Second)
	defer cancel()
	name := ""
	if len(names) > 0 {
		name = names[0]
	}
	i, err = a.DB.Select(ctx, i, name)
	if err != nil {
		return nil, err
	}
	if !i.Policy.AllowsDatabase(i.Connection.Database) || !b.Policy.AllowsDatabase(i.Connection.Database) {
		return nil, storage.ErrDenied
	}
	cols, err := a.DB.Catalog(ctx, i)
	if err != nil {
		return nil, err
	}
	return VisibleColumns(cols, i.Policy, b.Policy), nil
}

func VisibleColumns(cols []model.Column, policies ...model.Policy) []model.Column {
	out := []model.Column{}
	for _, c := range cols {
		if sqlguard.SystemSchema(c.Schema) {
			continue
		}
		s := model.Source{Schema: c.Schema, Table: c.Table, Column: c.Name}
		allowed := true
		for _, policy := range policies {
			if !policy.AllowsColumn(c.Database, c.Schema, c.Table, c.Name) || mask.Denied(policy.Masks, s) {
				allowed = false
				break
			}
		}
		if allowed {
			out = append(out, c)
		}
	}
	return out
}
func (a *App) prepare(ctx context.Context, id model.Identity, q model.Query) (p prepared, err error) {
	stage := "policy.check"
	defer func() {
		if err != nil {
			diagnostics.Event(ctx, slog.LevelWarn, "request.prepare_failed", "stage", stage, "entity_id", id.EntityID, "instance_id", q.InstanceID, "tool", q.Tool, "error", diagnostics.ErrorInfo(err))
		}
	}()
	p.instance, p.binding, err = a.policies(id, q.InstanceID)
	if err != nil {
		return p, err
	}
	base, extra := p.instance.Policy, p.binding.Policy
	if q.ClientRequestID == "" || len(q.ClientRequestID) > 200 {
		return p, errors.New("client_request_id is required (maximum 200 characters)")
	}
	view := effective(p.instance, p.binding)
	p.rows = view.MaxRows
	p.bytes = view.MaxBytes
	p.timeout = view.TimeoutSeconds
	cctx, cancel := context.WithTimeout(ctx, time.Duration(p.timeout)*time.Second)
	defer cancel()
	stage = "database.authorize"
	p.instance, err = a.DB.Select(cctx, p.instance, q.Database)
	if err != nil {
		return p, err
	}
	if !base.AllowsDatabase(p.instance.Connection.Database) || !extra.AllowsDatabase(p.instance.Connection.Database) {
		return p, storage.ErrDenied
	}
	stage = "schema.discover"
	catalog, err := a.DB.Catalog(cctx, p.instance)
	if err != nil {
		return p, err
	}
	stage = "sql.analyze"
	p.info, err = sqlguard.Analyze(p.instance.Driver, p.instance.Connection.Database, database.DefaultSchema(p.instance), q.SQL, catalog)
	if err != nil {
		return p, err
	}
	stage = "operation.authorize"
	if q.Tool == "db_query" && p.info.Operation != "select" {
		return p, errors.New("db_query accepts SELECT only")
	}
	if q.Tool == "db_execute" && p.info.Operation == "select" {
		return p, errors.New("db_execute accepts INSERT, UPDATE or DELETE only")
	}
	if q.Tool != "db_query" && q.Tool != "db_execute" && q.Tool != "db_explain" {
		return p, errors.New("unsupported database tool")
	}
	if q.Tool == "db_explain" && (p.instance.Driver != "postgres" || p.info.Operation != "select") {
		return p, errors.New("non-executing Explain currently supports PostgreSQL SELECT only")
	}
	if !slices.Contains(view.Operations, p.info.Operation) {
		return p, storage.ErrDenied
	}
	for _, t := range p.info.Tables {
		if !base.AllowsTable(p.instance.Connection.Database, t.Schema, t.Table) || !extra.AllowsTable(p.instance.Connection.Database, t.Schema, t.Table) {
			return p, storage.ErrDenied
		}
	}
	for _, s := range p.info.Reads {
		if !base.AllowsColumn(p.instance.Connection.Database, s.Schema, s.Table, s.Column) || !extra.AllowsColumn(p.instance.Connection.Database, s.Schema, s.Table, s.Column) || mask.Denied(base.Masks, s) || mask.Denied(extra.Masks, s) {
			return p, storage.ErrDenied
		}
	}
	p.approval = view.Approval == "always" || (view.Approval == "writes" && p.info.Operation != "select")
	if q.Tool == "db_explain" {
		p.info.Outputs = []model.Output{{Name: "plan", Sources: p.info.Reads, Derived: true}}
		p.info.ReturnsRows = true
	}
	if base.Approval != "always" && extra.Approval != "always" && len(p.info.Tables) > 0 {
		for _, rule := range p.binding.ApprovalRules {
			if rule.Operation != p.info.Operation || rule.Database != p.instance.Connection.Database {
				continue
			}
			all := true
			for _, table := range p.info.Tables {
				if !slices.Contains(rule.Tables, table) {
					all = false
					break
				}
			}
			if all {
				p.approval = false
				break
			}
		}
	}
	return p, nil
}
func (a *App) Simulate(ctx context.Context, id model.Identity, q model.Query) (any, error) {
	p, err := a.prepare(ctx, id, q)
	if err != nil {
		return nil, err
	}
	decision := "allowed"
	if p.approval {
		decision = "approval_required"
	}
	return map[string]any{"decision": decision, "database": p.instance.Connection.Database, "statement": p.info, "base": p.instance.Policy, "binding": p.binding.Policy}, nil
}
func digest(q model.Query) string {
	// Explanation is metadata, not part of the executable approval or retry identity.
	q.AgentReason = ""
	b, _ := json.Marshal(q)
	return storage.Hash(string(b))
}
func (a *App) Query(ctx context.Context, id model.Identity, q model.Query) (r model.Result, err error) {
	reason := []rune(strings.TrimSpace(q.AgentReason))
	q.AgentReason = string(reason[:min(100, len(reason))])
	start := time.Now()
	diagnostics.Event(ctx, slog.LevelInfo, "request.start", "entity_id", id.EntityID, "instance_id", q.InstanceID, "credential_id", id.CredentialID, "source", id.Source, "tool", q.Tool, "query_fingerprint", digest(q))
	defer func() {
		decision := r.Status
		if err != nil {
			decision = "denied"
		}
		diagnostics.Event(ctx, slog.LevelInfo, "request.complete", "entity_id", id.EntityID, "instance_id", q.InstanceID, "request_id", r.RequestID, "tool", q.Tool, "status", decision, "duration_ms", time.Since(start).Milliseconds(), "error", diagnostics.ErrorInfo(err))
		_ = a.Store.Audit(model.Audit{EntityID: id.EntityID, InstanceID: q.InstanceID, RequestID: r.RequestID, Action: q.Tool, Decision: decision, Detail: "request_digest=" + digest(q), DurationMS: time.Since(start).Milliseconds()})
		a.Changed()
	}()
	p, err := a.prepare(ctx, id, q)
	if err != nil {
		// Rejected requests also need a traceable history entry, without creating
		// executable work in the approval/retry state machine.
		r.RequestID = model.ID()
		a.recordRequestDetail(ctx, id, q, model.Request{ID: r.RequestID, Status: "denied", CreatedAt: start.UTC().Format(time.RFC3339Nano)})
		a.completeRequestDetail(ctx, r.RequestID, model.Result{Status: "denied", Reason: err.Error()}, start, nil)
		return r, err
	}
	q.Database = p.instance.Connection.Database
	status := "approved"
	if p.approval {
		status = "pending"
	}
	expires := time.Now().Add(time.Duration(min(p.instance.Policy.ApprovalSeconds, p.binding.Policy.ApprovalSeconds)) * time.Second).UTC().Format(time.RFC3339)
	v := model.Request{ID: model.ID(), EntityID: id.EntityID, CredentialID: id.CredentialID, InstanceID: q.InstanceID, Database: q.Database, ClientRequestID: q.ClientRequestID, Digest: digest(q), Status: status, Operation: p.info.Operation, Tables: p.info.Tables, CreatedAt: time.Now().UTC().Format(time.RFC3339), ExpiresAt: expires, Source: id.Source, InstanceRevision: p.instance.Revision, BindingRevision: p.binding.Revision}
	v, created, err := a.Store.CreateRequest(v, q)
	if err != nil {
		r.RequestID = model.ID()
		a.recordRequestDetail(ctx, id, q, model.Request{ID: r.RequestID, Status: "denied", CreatedAt: start.UTC().Format(time.RFC3339Nano)})
		a.completeRequestDetail(ctx, r.RequestID, model.Result{Status: "denied", Reason: err.Error()}, start, nil)
		return r, err
	}
	if !created {
		return a.Resume(ctx, id, v.ID)
	}
	a.recordRequestDetail(ctx, id, q, v)
	_ = a.Store.Audit(model.Audit{EntityID: id.EntityID, InstanceID: q.InstanceID, RequestID: v.ID, Action: "request.received", Decision: "received"})
	_ = a.Store.Audit(model.Audit{EntityID: id.EntityID, InstanceID: q.InstanceID, RequestID: v.ID, Action: "policy.checked", Decision: "allowed"})
	a.Changed()
	if v.Status == "pending" {
		diagnostics.Event(ctx, slog.LevelInfo, "approval.required", "entity_id", id.EntityID, "instance_id", q.InstanceID, "request_id", v.ID, "instance_revision", v.InstanceRevision, "binding_revision", v.BindingRevision)
		return requestResult(v), nil
	}
	return a.execute(ctx, id, v, q)
}
func requestResult(v model.Request) model.Result {
	if v.Result != nil {
		return *v.Result
	}
	r := model.Result{RequestID: v.ID, Status: v.Status, ExpiresAt: v.ExpiresAt, Reason: v.Reason}
	if v.Status == "succeeded" {
		r.Reason = "cached result is no longer available; completed writes must not be replayed"
	}
	return r
}
func (a *App) checkedResult(id model.Identity, v model.Request) (model.Result, error) {
	in, b, err := a.policies(id, v.InstanceID)
	if err != nil {
		return model.Result{}, err
	}
	if v.Result != nil && (v.InstanceRevision != in.Revision || v.BindingRevision != b.Revision) {
		return model.Result{RequestID: v.ID, Status: v.Status, Reason: "policy changed; cached result is unavailable and completed writes will not be replayed"}, nil
	}
	return requestResult(v), nil
}
func (a *App) Status(id model.Identity, requestID string) (model.Result, error) {
	if err := a.Store.Recheck(id); err != nil {
		return model.Result{}, err
	}
	v, err := a.Store.Request(requestID, false)
	if err != nil {
		return model.Result{}, err
	}
	if v.EntityID != id.EntityID || v.CredentialID != id.CredentialID {
		return model.Result{}, storage.ErrDenied
	}
	return a.checkedResult(id, v)
}
func (a *App) Resume(ctx context.Context, id model.Identity, requestID string) (model.Result, error) {
	if err := a.Store.Recheck(id); err != nil {
		return model.Result{}, err
	}
	v, err := a.Store.Request(requestID, true)
	if err != nil {
		return model.Result{}, err
	}
	if v.EntityID != id.EntityID || v.CredentialID != id.CredentialID {
		return model.Result{}, storage.ErrDenied
	}
	if v.Status != "approved" {
		return a.checkedResult(id, v)
	}
	if v.Query == nil {
		return model.Result{}, errors.New("request payload unavailable")
	}
	return a.execute(ctx, id, v, *v.Query)
}
func (a *App) execute(ctx context.Context, id model.Identity, v model.Request, q model.Query) (model.Result, error) {
	start := time.Now()
	diagnostics.Event(ctx, slog.LevelInfo, "execution.recheck", "entity_id", id.EntityID, "instance_id", v.InstanceID, "request_id", v.ID)
	if digest(q) != v.Digest {
		return model.Result{}, storage.ErrConflict
	}
	// Pre-1.0.1 encrypted payloads omitted the database. Migration pins their
	// request body, while preserving the old payload digest for verification.
	if q.Database == "" {
		q.Database = v.Database
	}
	if q.Database == "" || q.Database != v.Database {
		return model.Result{}, storage.ErrConflict
	}
	// Recheck the current credential, resources, SQL and masks immediately before claim.
	p, err := a.prepare(ctx, id, q)
	if err != nil {
		_ = a.Store.Audit(model.Audit{EntityID: id.EntityID, InstanceID: q.InstanceID, RequestID: v.ID, Action: "execution.recheck", Decision: "denied"})
		return model.Result{}, err
	}
	v.InstanceRevision = p.instance.Revision
	v.BindingRevision = p.binding.Revision
	// A concurrent retry can reach claim before the original caller records its
	// snapshot. Insert-only history must exist before either caller executes.
	a.recordRequestDetail(ctx, id, q, v)
	if err = a.Store.Claim(v); err != nil {
		diagnostics.Event(ctx, slog.LevelInfo, "execution.already_claimed", "request_id", v.ID, "error", diagnostics.ErrorInfo(err))
		current, e := a.Store.Request(v.ID, false)
		if e != nil {
			return model.Result{}, e
		}
		return a.checkedResult(id, current)
	}
	_ = a.Store.Audit(model.Audit{EntityID: id.EntityID, InstanceID: q.InstanceID, RequestID: v.ID, Action: "execution.start", Decision: "executing"})
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.timeout)*time.Second)
	defer cancel()
	diagnostics.Event(ctx, slog.LevelInfo, "execution.start", "request_id", v.ID, "driver", p.instance.Driver, "database", p.instance.Connection.Database, "timeout_seconds", p.timeout, "max_rows", p.rows, "max_bytes", p.bytes, "instance_revision", p.instance.Revision, "binding_revision", p.binding.Revision, "mask_rules", len(p.instance.Policy.Masks)+len(p.binding.Policy.Masks))
	matched := []model.MaskMatch{}
	r, err := a.DB.Execute(ctx, p.instance, q, p.info, p.rows, p.bytes, min(p.instance.Policy.Concurrency, p.binding.Policy.Concurrency), func(n int, x any) (any, error) {
		if n >= len(p.info.Outputs) {
			return nil, errors.New("unresolved result source")
		}
		value, matches := mask.ValueWithMatches(p.info.Outputs[n], x, p.instance.Policy, p.binding.Policy)
		for _, match := range matches {
			if !slices.Contains(matched, match) {
				matched = append(matched, match)
			}
		}
		return value, nil
	})
	r.RequestID = v.ID
	if err != nil {
		r = model.Result{RequestID: v.ID, Status: "failed", Reason: err.Error()}
	}
	if e := a.Store.Finish(v.ID, r); e != nil {
		diagnostics.Event(ctx, slog.LevelError, "execution.outcome_unknown", "request_id", v.ID, "error", diagnostics.ErrorInfo(e))
		unknown := model.Result{RequestID: v.ID, Status: "result_unknown", Reason: "cannot persist execution outcome; inspect the target database"}
		a.completeRequestDetail(ctx, v.ID, unknown, start, matched)
		return unknown, nil
	}
	a.completeRequestDetail(ctx, v.ID, r, start, matched)
	diagnostics.Event(ctx, slog.LevelInfo, "execution.complete", "request_id", v.ID, "status", r.Status, "error", diagnostics.ErrorInfo(err))
	_ = a.Store.Audit(model.Audit{EntityID: id.EntityID, InstanceID: q.InstanceID, RequestID: v.ID, Action: "execute", Decision: r.Status, DurationMS: time.Since(start).Milliseconds()})
	a.Changed()
	return r, nil
}
func (a *App) Decide(requestID, decision string) error {
	if decision == "remember" {
		return a.remember(requestID)
	}
	to := "denied"
	if decision == "approve" {
		to = "approved"
	} else if decision != "deny" {
		return errors.New("decision must be approve or deny")
	}
	v, err := a.Store.Request(requestID, false)
	if err != nil {
		return err
	}
	if v.Status != "pending" {
		return storage.ErrConflict
	}
	if err = a.Store.Transition(requestID, "pending", to); err != nil {
		return err
	}
	err = a.Store.Audit(model.Audit{EntityID: v.EntityID, InstanceID: v.InstanceID, RequestID: v.ID, Action: "approval", Decision: to})
	a.Changed()
	return err
}

func (a *App) remember(id string) error {
	v, err := a.Store.Request(id, true)
	if err != nil {
		return err
	}
	if v.Status != "pending" || v.Query == nil {
		return storage.ErrConflict
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	query := *v.Query
	if query.Database == "" {
		query.Database = v.Database
	}
	p, err := a.prepare(ctx, model.Identity{EntityID: v.EntityID, CredentialID: v.CredentialID, Source: v.Source}, query)
	if err != nil {
		return err
	}
	if p.instance.Policy.Approval == "always" || p.binding.Policy.Approval == "always" {
		return errors.New("always-ask policy requires an explicit policy change before remembering approvals")
	}
	if len(p.info.Tables) == 0 {
		return errors.New("a remembered rule needs explicit tables")
	}
	rule := model.ApprovalRule{ID: model.ID(), Database: p.instance.Connection.Database, Operation: p.info.Operation, Tables: p.info.Tables}
	if err = ValidateApprovalRules([]model.ApprovalRule{rule}); err != nil {
		return err
	}
	p.binding.ApprovalRules = append(p.binding.ApprovalRules, rule)
	if err = a.Store.RememberAndApprove(ctx, id, p.binding); err != nil {
		return err
	}
	err = a.Store.Audit(model.Audit{EntityID: v.EntityID, InstanceID: v.InstanceID, RequestID: id, Action: "approval.remember", Decision: "approved", Detail: "operation=" + rule.Operation})
	a.Changed()
	return err
}
