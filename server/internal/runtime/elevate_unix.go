//go:build !windows

package runtime

import (
	"context"
	"encoding/json"
	"errors"
)

func Elevate(context.Context, []string) (json.RawMessage, error) {
	return nil, errors.New("run system service commands with the platform's explicit administrator authorization; user services do not require elevation")
}
