//go:build !dm

package database

import (
	"dbmcp.local/server/internal/model"
	"errors"
)

const DMAvailable = false

func nativeValue(any, int) (any, bool, error) { return nil, false, nil }

func dmDSN(model.Instance) (string, string, error) {
	return "", "", errors.New("this build does not include the DM8 native driver; use the native build described in the driver documentation")
}
