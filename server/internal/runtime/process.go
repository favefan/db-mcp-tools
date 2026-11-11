package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func Status(ctx context.Context, dir string) (map[string]any, error) {
	b, err := AdminRequest(ctx, dir, "", "", "GET", "/admin/v1/health", nil)
	if err != nil {
		return map[string]any{"running": false}, nil
	}
	var out map[string]any
	if err = json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	actual, _ := out["data_dir"].(string)
	if actual == "" || !sameDirectory(dir, actual) {
		return nil, errors.New("the listener belongs to a different data directory; refusing local control")
	}
	out["running"] = true
	return out, nil
}

func sameDirectory(a, b string) bool {
	normalize := func(p string) string {
		if absolute, err := filepath.Abs(p); err == nil {
			p = absolute
		}
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		return filepath.Clean(p)
	}
	a, b = normalize(a), normalize(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
func Start(ctx context.Context, dir string) error {
	if native, err := serviceInstalled(dir, "", false); err != nil {
		return err
	} else if native {
		return errors.New("this data directory is registered as a native service; use service start instead of run start")
	}
	if _, err := Initialize(dir); err != nil {
		status, e := Status(ctx, dir)
		if e == nil && status["running"] == true {
			return errors.New("Server already running")
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if installed, e := InstalledExecutable(dir); e == nil {
		exe = installed
	} else if !os.IsNotExist(e) {
		return e
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(dir, "server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(exe, "--data-dir", dir, "serve", "--mode", "background")
	cmd.Stdout = log
	cmd.Stderr = log
	detach(cmd)
	if err = cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Server startup timed out; inspect server.log")
		case <-ticker.C:
			status, err := Status(ctx, dir)
			if err == nil && status["running"] == true {
				return nil
			}
		}
	}
}
func Stop(ctx context.Context, dir string) error {
	c, err := LoadConfig(dir)
	if err != nil {
		return err
	}
	status, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if status["running"] != true {
		return nil
	}
	if status["mode"] != "background" {
		return errors.New("use the original terminal or OS service manager to stop this Server")
	}
	b, err := os.ReadFile(filepath.Join(dir, "admin.token"))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+c.Admin+"/admin/v1/runtime/stop", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(b))
	req.Header.Set("X-DBMCP-Server-ID", status["server_id"].(string))
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("Server refused stop")
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(25 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Server did not stop within 25 seconds")
		case <-ticker.C:
			st, _ := Status(ctx, dir)
			if st["running"] != true {
				return nil
			}
		}
	}
}
