package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"dbmcp.local/server/internal/diagnostics"
	"dbmcp.local/server/internal/model"
)

func (a *App) recordRequestDetail(ctx context.Context, id model.Identity, q model.Query, v model.Request) {
	params, _ := json.Marshal(q.Params)
	detail := model.RequestDetail{ID: v.ID, Tool: q.Tool, ClientRequestID: q.ClientRequestID, EntityID: id.EntityID, InstanceID: q.InstanceID, Database: q.Database, Source: id.Source, TraceID: diagnostics.Trace(ctx), SQL: q.SQL, AgentReason: q.AgentReason, ParameterJSON: string(params), Status: v.Status, Operation: v.Operation, Tables: v.Tables, CreatedAt: v.CreatedAt}
	if entity, err := a.Store.Entity(id.EntityID); err == nil {
		detail.EntityName, detail.Client = entity.Name, entity.Client
	}
	if instance, err := a.Store.Instance(q.InstanceID, false); err == nil {
		detail.InstanceName = instance.Name
	}
	if err := a.Store.RecordRequestDetail(detail); err != nil {
		diagnostics.Event(ctx, slog.LevelError, "request_detail.write_failed", "request_id", v.ID, "error", diagnostics.ErrorInfo(err))
	}
}

func (a *App) completeRequestDetail(ctx context.Context, id string, r model.Result, start time.Time, matches []model.MaskMatch) {
	summary := model.ExecutionSummary{Columns: r.Columns, RowCount: len(r.Rows), Affected: r.Affected, Truncated: r.Truncated, Reason: r.Reason, Masks: matches}
	if err := a.Store.CompleteRequestDetail(id, r.Status, summary, time.Since(start).Milliseconds()); err != nil {
		diagnostics.Event(ctx, slog.LevelError, "request_detail.write_failed", "request_id", id, "error", diagnostics.ErrorInfo(err))
	}
}
