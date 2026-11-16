package app

import (
	"context"
	"errors"

	"dbmcp.local/server/internal/database"
	"dbmcp.local/server/internal/model"
)

// Events contain only administrator-visible summaries, never SQL or parameters.
type Event struct {
	Failure *DatabaseFailure
}

type DatabaseFailure struct {
	RequestID    string `json:"request_id"`
	InstanceID   string `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	Database     string `json:"database"`
	Phase        string `json:"phase"`
	Reason       string `json:"reason"`
}

func failureStatus(err error) string {
	var failure *database.Failure
	if errors.As(err, &failure) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "failed"
	}
	return "denied"
}

func (a *App) databaseFailed(q model.Query, requestID, phase string, err error) {
	var failure *database.Failure
	if !errors.As(err, &failure) {
		return
	}
	i, e := a.Store.Instance(q.InstanceID, false)
	if e != nil {
		return
	}
	if q.Database == "" {
		q.Database = i.Connection.Database
	}
	a.publish(Event{Failure: &DatabaseFailure{RequestID: requestID, InstanceID: i.ID, InstanceName: i.Name, Database: q.Database, Phase: phase, Reason: failure.Error()}})
}
