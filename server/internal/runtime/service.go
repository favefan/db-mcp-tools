package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kardianos/service"
)

type program struct {
	dir    string
	cancel context.CancelFunc
	done   chan error
}

func (p *program) Start(service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan error, 1)
	ready := make(chan error, 1)
	decision := make(chan bool, 1)
	go func() {
		err := serve(ctx, p.dir, "service", ready)
		p.done <- err
		if started := <-decision; started && err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "MCP Server service failed:", err)
			os.Exit(1)
		}
	}()
	select {
	case err := <-ready:
		decision <- err == nil
		return err
	case <-time.After(20 * time.Second):
		cancel()
		decision <- false
		return errors.New("service startup timed out")
	}
}
func (p *program) Stop(service.Service) error {
	p.cancel()
	select {
	case err := <-p.done:
		return err
	case <-time.After(25 * time.Second):
		return errors.New("service shutdown timed out")
	}
}
func Service(dir, action, username string, system bool) (any, error) {
	if action == "status" && service.ChosenSystem() == nil {
		return map[string]any{"installed": false, "state": "unsupported"}, nil
	}
	switch action {
	case "install", "uninstall", "start", "stop", "restart", "status", "run":
	default:
		return nil, errors.New("unknown service action")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	identity := dir
	if runtime.GOOS == "windows" {
		identity = strings.ToLower(identity)
	}
	h := sha256.Sum256([]byte(identity))
	name := "DBMCP-" + hex.EncodeToString(h[:6])
	opts := service.KeyValue{"UserService": !system, "RunAtLoad": true, "Restart": "on-failure", "OnFailure": "restart", "OnFailureDelayDuration": "5s"}
	if runtime.GOOS == "windows" && username == "" {
		username = `NT AUTHORITY\LocalService`
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if action == "install" {
		if runtime.GOOS != "windows" && system && (username == "" || username == "root") {
			return nil, errors.New("system services require an explicit non-root --user account")
		}
		if _, err = Initialize(dir); err != nil {
			return nil, err
		}
		executable, err = InstalledExecutable(dir)
		if os.IsNotExist(err) {
			if _, err = Deploy(context.Background(), dir); err != nil {
				return nil, err
			}
			executable, err = InstalledExecutable(dir)
		}
		if err != nil {
			return nil, err
		}
		if err = prepareServiceDir(dir, username); err != nil {
			return nil, err
		}
	}
	s, err := service.New(&program{dir: dir}, &service.Config{Name: name, DisplayName: "DB MCP Tools", Description: "Scoped database MCP Server", Executable: executable, UserName: username, Arguments: []string{"--data-dir", dir, "service", "run"}, WorkingDirectory: dir, Option: opts})
	if err != nil {
		return nil, err
	}
	if action == "status" {
		v, err := s.Status()
		if err != nil {
			var installed any
			if errors.Is(err, service.ErrNotInstalled) {
				installed = false
			}
			return map[string]any{"name": name, "state": "unavailable", "installed": installed, "message": err.Error()}, nil
		}
		status, _ := Status(context.Background(), dir)
		ready := status["running"] == true && status["mode"] == "service"
		state := map[service.Status]string{service.StatusRunning: "running", service.StatusStopped: "stopped", service.StatusUnknown: "unknown"}[v]
		if state == "running" && !ready {
			state = "starting"
		}
		if state == "stopped" && ready {
			state = "stopping"
		}
		return map[string]any{"name": name, "state": state, "installed": true, "ready": ready}, nil
	}
	if action == "run" {
		return nil, s.Run()
	}
	control := func(action string) error {
		controlled := make(chan error, 1)
		go func() { controlled <- service.Control(s, action) }()
		select {
		case err := <-controlled:
			return err
		case <-time.After(35 * time.Second):
			return errors.New("OS service control timed out; inspect the native service manager")
		}
	}
	if action == "uninstall" {
		if status, e := s.Status(); e == nil && status == service.StatusRunning {
			if err = control("stop"); err != nil {
				return nil, err
			}
		}
	}
	if err = control(action); err != nil {
		return nil, err
	}
	if action == "start" || action == "restart" {
		deadline := time.NewTimer(25 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			status, err := Status(context.Background(), dir)
			if err != nil {
				return nil, err
			}
			if status["running"] == true && status["mode"] == "service" {
				break
			}
			select {
			case <-deadline.C:
				return nil, errors.New("service did not become ready; inspect its startup error in the OS service log")
			case <-ticker.C:
			}
		}
	}
	out := map[string]any{"name": name, "action": action, "data_dir": dir, "executable": executable, "launcher": filepath.Join(runtimeDir(dir), executableName())}
	if action == "start" || action == "restart" {
		out["status"], _ = Status(context.Background(), dir)
	}
	if config, e := LoadConfig(dir); e == nil {
		out["admin"] = config.Admin
	}
	return out, nil
}
