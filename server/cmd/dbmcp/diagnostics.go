package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"dbmcp.local/server/internal/diagnostics"
	run "dbmcp.local/server/internal/runtime"
)

func exportDiagnostics(ctx context.Context, dir, endpoint, destination string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out := map[string]any{"format": "dbmcp-diagnostics/v1", "created_at": time.Now().UTC().Format(time.RFC3339Nano), "build": diagnostics.Build(), "process_logs": diagnostics.Collect(diagnostics.UserDir()), "server_logs": diagnostics.Collect(filepath.Join(dir, "logs")), "server_status": "unavailable"}
	b, err := run.AdminRequest(ctx, dir, endpoint, os.Getenv("DBMCP_ADMIN_TOKEN"), "GET", "/admin/v1/diagnostics", nil)
	if err != nil {
		out["server_collection_error"] = diagnostics.ErrorInfo(err)
		out["missing"] = []string{"live MCP Server snapshot unavailable; local files only"}
	} else {
		var snapshot any
		if json.Unmarshal(b, &snapshot) != nil {
			return errors.New("invalid diagnostic snapshot")
		}
		out["server"] = snapshot
		out["server_status"] = "collected"
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	if destination == filepath.Join(dir, "state.sqlite") || destination == filepath.Join(dir, "admin.token") || destination == filepath.Join(dir, "key") {
		return errors.New("diagnostic output must not replace MCP Server state")
	}
	// Exclusive creation also prevents following a symlink or overwriting any
	// existing state/configuration selected accidentally on a headless machine.
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	err = encoder.Encode(out)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return output(map[string]any{"path": destination, "server_status": out["server_status"]})
}
