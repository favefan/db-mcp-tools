package database

import (
	"context"
	"errors"
	"net"

	"dbmcp.local/server/internal/model"
)

// Failure distinguishes database access failures from authorization denials.
// Only the fixed, safe message is exposed; diagnostics can inspect the cause.
type Failure struct {
	Stage   string
	message string
	cause   error
}

func (e *Failure) Error() string { return e.message }
func (e *Failure) Unwrap() error { return e.cause }

func failure(ctx context.Context, i model.Instance, stage, message string, err error) error {
	diagnosticFailure(ctx, i, stage, err)
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		message += "; connection or database operation timed out"
	} else {
		var operation *net.OpError
		if errors.As(err, &operation) {
			message += "; network connection failed"
		}
	}
	return &Failure{Stage: stage, message: message, cause: err}
}
