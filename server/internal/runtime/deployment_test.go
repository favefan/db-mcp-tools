package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dbmcp.local/server/internal/storage"
)

func TestPreparationSkipsBackupButUpgradeRetainsSafetySnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := InitializePorts(dir, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(ctx, dir); err != nil {
		t.Fatal(err)
	}
	state, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	backups, err := state.Backups()
	state.Close()
	if err != nil || len(backups) != 0 {
		t.Fatalf("initial preparation created a backup: %v %v", backups, err)
	}
	if _, err = Prepare(ctx, dir); err == nil {
		t.Fatal("preparation replaced an existing deployment")
	}
	old, err := readManifest(runtimeDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(runtimeDir(dir), "versions", "old", executableName())
	if err = os.MkdirAll(filepath.Dir(oldFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(oldFile, []byte("old-program"), 0700); err != nil {
		t.Fatal(err)
	}
	old.Executable = filepath.Join("versions", "old", executableName())
	old.SHA256, err = digestFile(oldFile)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeManifest(filepath.Join(runtimeDir(dir), "current.json"), old); err != nil {
		t.Fatal(err)
	}
	result, err := Deploy(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	backup, ok := result["backup"].(string)
	if !ok || backup == "" {
		t.Fatal("explicit upgrade lost its safety snapshot")
	}
	if _, err = storage.ValidateBackup(backup); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRejectsEscapedPathsAndCorruptedExecutables(t *testing.T) {
	root := t.TempDir()
	m := RuntimeManifest{Format: 1, API: 1, Executable: "../outside", SHA256: "irrelevant"}
	if err := writeManifest(filepath.Join(root, "current.json"), m); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(root); err == nil {
		t.Fatal("launcher accepted a path outside its runtime")
	}
	m.Executable = "versions/program"
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, m.Executable)
	if err := os.WriteFile(path, []byte("known executable"), 0700); err != nil {
		t.Fatal(err)
	}
	var err error
	m.SHA256, err = digestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resolveManifest(root, m); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("corrupted executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = resolveManifest(root, m); err == nil {
		t.Fatal("launcher ran a corrupted executable")
	}
}
