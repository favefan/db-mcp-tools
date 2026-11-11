package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

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
