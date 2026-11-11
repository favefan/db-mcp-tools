package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"dbmcp.local/server/internal/database"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
)

type RuntimeManifest struct {
	Format     int             `json:"format"`
	Version    string          `json:"version"`
	API        int             `json:"api_version"`
	Executable string          `json:"executable"`
	SHA256     string          `json:"sha256"`
	Drivers    map[string]bool `json:"drivers"`
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "dbmcp.exe"
	}
	return "dbmcp"
}
func runtimeDir(dir string) string { return filepath.Join(dir, "runtime") }
func digestFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func readManifest(root string) (RuntimeManifest, error) {
	bytes, err := os.ReadFile(filepath.Join(root, "current.json"))
	if err != nil {
		return RuntimeManifest{}, err
	}
	return parsePrevious(bytes)
}
func parsePrevious(bytes []byte) (RuntimeManifest, error) {
	var m RuntimeManifest
	var err error
	if err = json.Unmarshal(bytes, &m); err != nil {
		return m, errors.New("invalid installed runtime manifest")
	}
	if m.Format != 1 || m.API != model.APIVersion || !filepath.IsLocal(m.Executable) || m.SHA256 == "" {
		return m, errors.New("installed runtime is incompatible or its path is invalid")
	}
	return m, nil
}
func resolveManifest(root string, m RuntimeManifest) (string, error) {
	target := filepath.Join(root, m.Executable)
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(base, real)
	if err != nil || !filepath.IsLocal(relative) {
		return "", errors.New("installed executable leaves the runtime directory")
	}
	hash, err := digestFile(real)
	if err != nil {
		return "", err
	}
	if hash != m.SHA256 {
		return "", errors.New("installed executable checksum mismatch; redeploy the selected MCP Server")
	}
	return real, nil
}
func InstalledExecutable(dir string) (string, error) {
	root := runtimeDir(dir)
	m, err := readManifest(root)
	if err != nil {
		return "", err
	}
	return resolveManifest(root, m)
}

// Agent paths remain stable. SCM points directly at a versioned executable:
// the service dispatcher must run in the process owned by SCM.
func ForwardInstalled() (bool, int, error) {
	executable, err := os.Executable()
	if err != nil {
		return false, 0, err
	}
	root := filepath.Dir(executable)
	m, err := readManifest(root)
	if os.IsNotExist(err) {
		return false, 0, nil
	}
	if err != nil {
		return true, 1, err
	}
	target, err := resolveManifest(root, m)
	if err != nil {
		return true, 1, err
	}
	if sameDirectory(filepath.Dir(target), root) {
		return true, 1, errors.New("installed launcher would call itself")
	}
	cmd := exec.Command(target, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	hideChild(cmd)
	if err = cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return true, exit.ExitCode(), nil
		}
		return true, 1, err
	}
	return true, 0, nil
}

// Deploy requires a stopped data directory and saves a consistent rollback
// snapshot. Versioned files avoid replacing an executable still open on Windows.
func Deploy(ctx context.Context, dir string) (map[string]any, error) { return deploy(ctx, dir, true) }

// Prepare is only for the first managed launch; routine starts never create backups.
func Prepare(ctx context.Context, dir string) (map[string]any, error) {
	if _, err := os.Stat(filepath.Join(runtimeDir(dir), "current.json")); err == nil {
		return nil, errors.New("runtime is already prepared; use runtime upgrade")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return deploy(ctx, dir, false)
}

func deploy(ctx context.Context, dir string, snapshot bool) (map[string]any, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	source, err := os.Executable()
	if err != nil {
		return nil, err
	}
	hash, err := digestFile(source)
	if err != nil {
		return nil, err
	}
	root := runtimeDir(dir)
	launcher := filepath.Join(root, executableName())
	previous, oldErr := readManifest(root)
	if oldErr == nil && previous.SHA256 == hash {
		installed, err := resolveManifest(root, previous)
		info, statErr := os.Stat(launcher)
		if err == nil && statErr == nil && info.Mode().IsRegular() {
			return map[string]any{"executable": installed, "launcher": launcher, "version": previous.Version, "unchanged": true}, nil
		}
	} else if oldErr != nil && !os.IsNotExist(oldErr) {
		return nil, oldErr
	}
	s, err := storage.Open(dir)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	var backup any
	if snapshot {
		backup, err = s.Backup(ctx)
		if err != nil {
			return nil, err
		}
	}

	relative := filepath.Join("versions", model.Version+"-"+hash[:12], executableName())
	target := filepath.Join(root, relative)
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return nil, err
	}
	if existing, e := digestFile(target); e != nil || existing != hash {
		if err = copyAtomic(source, target); err != nil {
			return nil, err
		}
		if err = os.Chmod(target, 0700); err != nil {
			return nil, err
		}
	}
	if _, err = os.Stat(launcher); os.IsNotExist(err) {
		if err = copyAtomic(source, launcher); err != nil {
			return nil, err
		}
		if err = os.Chmod(launcher, 0700); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if info, err := os.Stat(launcher); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("stable launcher path is not a regular file")
	}
	next := RuntimeManifest{Format: 1, Version: model.Version, API: model.APIVersion, Executable: relative, SHA256: hash, Drivers: map[string]bool{"postgres": true, "mysql": true, "sqlserver": true, "dm": database.DMAvailable}}
	if oldErr == nil {
		if err = writeManifest(filepath.Join(root, "previous.json"), previous); err != nil {
			return nil, err
		}
	}
	if err = writeManifest(filepath.Join(root, "current.json"), next); err != nil {
		return nil, err
	}
	return map[string]any{"executable": target, "launcher": launcher, "version": model.Version, "backup": backup, "previous_manifest": filepath.Join(root, "previous.json"), "drivers": next.Drivers}, nil
}
func writeManifest(path string, m RuntimeManifest) error {
	bytes, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".runtime-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(bytes); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
func VersionInfo() map[string]any {
	return map[string]any{"version": model.Version, "api_version": model.APIVersion, "platform": runtime.GOOS, "arch": runtime.GOARCH, "default_data_dir": DefaultDir(), "system_data_dir": SystemDir(), "drivers": map[string]bool{"postgres": true, "mysql": true, "sqlserver": true, "dm": database.DMAvailable}}
}

func SystemDir() string {
	switch runtime.GOOS {
	case "windows":
		if root := os.Getenv("PROGRAMDATA"); root != "" {
			return filepath.Join(root, "DBMCP", "server")
		}
		return `C:\ProgramData\DBMCP\server`
	case "darwin":
		return "/Library/Application Support/DBMCP/server"
	default:
		return "/var/lib/dbmcp"
	}
}
