package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"dbmcp.local/server/internal/storage"
	"github.com/gofrs/flock"
	"github.com/kardianos/service"
)

func serviceInstalled(dir, username string, system bool) (bool, error) {
	if service.ChosenSystem() == nil {
		return false, nil
	}
	value, err := Service(dir, "status", username, system)
	if err != nil {
		return false, err
	}
	status := value.(map[string]any)
	if installed, ok := status["installed"].(bool); ok {
		return installed, nil
	}
	return false, fmt.Errorf("cannot determine native service ownership: %v", status["message"])
}
func stopManaged(ctx context.Context, dir, username string, system bool) (running, native bool, err error) {
	status, err := Status(ctx, dir)
	if err != nil {
		return false, false, err
	}
	native, err = serviceInstalled(dir, username, system)
	if err != nil {
		return false, false, err
	}
	running = status["running"] == true
	if running {
		switch status["mode"] {
		case "background":
			err = Stop(ctx, dir)
		case "service":
			_, err = Service(dir, "stop", username, system)
		default:
			err = errors.New("stop the foreground MCP Server in its original terminal before deploying")
		}
	} else if native {
		// Also stop a service still starting or recovering from startup failure.
		value, _ := Service(dir, "status", username, system)
		state := value.(map[string]any)["state"]
		if state == "running" || state == "starting" {
			_, err = Service(dir, "stop", username, system)
		}
	}
	return running, native, err
}

// Upgrade coordinates one explicitly requested lifecycle operation. A failed
// launch keeps the previous executable and snapshot for deliberate recovery;
// it never rolls back state after MCP clients might have written to a target DB.
func Upgrade(ctx context.Context, dir, username string, system, start, asService bool) (out map[string]any, err error) {
	running, native, err := stopManaged(ctx, dir, username, system)
	if err != nil {
		return nil, err
	}
	if _, err = Initialize(dir); err != nil {
		return nil, err
	}
	out, err = Deploy(ctx, dir)
	if err != nil {
		return nil, err
	}
	if out["backup"] == nil && (native || asService) {
		s, e := storage.Open(dir)
		if e != nil {
			return out, e
		}
		backup, e := s.Backup(ctx)
		s.Close()
		if e != nil {
			return out, e
		}
		out["backup"] = backup
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("upgrade incomplete: %w; retained recovery snapshot: %v; previous executable manifest: %s", err, out["backup"], filepath.Join(runtimeDir(dir), "previous.json"))
		}
	}()
	if native {
		if _, err = Service(dir, "uninstall", username, system); err != nil {
			return out, err
		}
	}
	if native || asService {
		if _, err = Service(dir, "install", username, system); err != nil {
			return out, err
		}
		if running || start {
			_, err = Service(dir, "start", username, system)
		}
		out["mode"] = "service"
	} else {
		if running || start {
			err = Start(ctx, dir)
		}
		out["mode"] = "background"
	}
	out["data_dir"], _ = filepath.Abs(dir)
	if config, e := LoadConfig(dir); e == nil {
		out["admin"] = config.Admin
	}
	if err == nil {
		out["status"], err = Status(ctx, dir)
	}
	return out, err
}

func RollbackProgram(dir string) (map[string]any, error) {
	s, err := storage.Open(dir)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	root := runtimeDir(s.Dir)
	bytes, err := os.ReadFile(filepath.Join(root, "previous.json"))
	if err != nil {
		return nil, err
	}
	previous, err := parsePrevious(bytes)
	if err != nil {
		return nil, err
	}
	if _, err = resolveManifest(root, previous); err != nil {
		return nil, err
	}
	current, err := readManifest(root)
	if err != nil {
		return nil, err
	}
	if err = writeManifest(filepath.Join(root, "failed.json"), current); err != nil {
		return nil, err
	}
	if err = writeManifest(filepath.Join(root, "current.json"), previous); err != nil {
		return nil, err
	}
	return map[string]any{"launcher": filepath.Join(root, executableName()), "version": previous.Version, "next_step": "restore a matching snapshot if schema changed, then reinstall any native service so its executable path follows this selection"}, nil
}

// Migrate leaves the source and its snapshot intact and stopped. A nonempty
// destination is never overwritten, even if it appears to be an older copy.
func Migrate(ctx context.Context, source, target, username string, system, asService, start bool) (map[string]any, error) {
	if sameDirectory(source, target) {
		return Upgrade(ctx, source, username, system, start, asService)
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(source, target)
	if err == nil && filepath.IsLocal(relative) {
		return nil, errors.New("migration destination must be outside the source directory")
	}
	entries, err := os.ReadDir(target)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(entries) > 0 {
		return nil, errors.New("migration destination is not empty; existing state was preserved")
	}
	_, native, err := stopManaged(ctx, source, username, system)
	if err != nil {
		return nil, err
	}
	s, err := storage.Open(source)
	if err != nil {
		return nil, err
	}
	backup, err := s.Backup(ctx)
	s.Close()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(target, 0700); err != nil {
		return nil, err
	}
	if err = storage.Protect(target); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(target, "server.lock"))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return nil, errors.New("migration destination is in use")
	}
	defer lock.Unlock()
	for _, name := range []string{"state.sqlite", "key", "admin.token"} {
		if err = copyAtomic(filepath.Join(backup, name), filepath.Join(target, name)); err != nil {
			return nil, fmt.Errorf("migration copy failed; source snapshot retained at %s: %w", backup, err)
		}
	}
	if _, err = os.Stat(filepath.Join(source, "deployment.toml")); err == nil {
		if err = copyAtomic(filepath.Join(source, "deployment.toml"), filepath.Join(target, "deployment.toml")); err != nil {
			return nil, err
		}
	}
	if err = lock.Unlock(); err != nil {
		return nil, err
	}
	// The old service must not start a second owner at the next login/reboot.
	if native {
		if _, err = Service(source, "uninstall", username, system); err != nil {
			return nil, err
		}
	}
	out, err := Upgrade(ctx, target, username, system, start, asService)
	if err != nil {
		return nil, fmt.Errorf("destination startup failed; source retained at %s and snapshot at %s: %w", source, backup, err)
	}
	out["source_dir"], out["source_backup"] = source, backup
	return out, nil
}
