package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDeleteBackupPreservesOtherSnapshotsAndLiveState(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	key, _ := os.ReadFile(filepath.Join(s.Dir, "key"))
	token, _ := os.ReadFile(filepath.Join(s.Dir, "admin.token"))
	for _, id := range []string{"", ".", "..", first, "../state.sqlite", `..\state.sqlite`, "state.sqlite:stream"} {
		if err := s.DeleteBackup(id); !errors.Is(err, ErrInvalidBackup) {
			t.Fatalf("unsafe backup ID %q: %v", id, err)
		}
	}
	if err := s.DeleteBackup(filepath.Base(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatal("selected backup remains", err)
	}
	if err := s.DeleteBackup(filepath.Base(first)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("missing backup should report not found", err)
	}
	list, err := s.Backups()
	if err != nil || len(list) != 1 || list[0].ID != filepath.Base(second) {
		t.Fatal("remaining backup changed", list, err)
	}
	if _, err := ValidateBackup(second); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{"key": string(key), "admin.token": string(token)} {
		actual, err := os.ReadFile(filepath.Join(s.Dir, name))
		if err != nil || string(actual) != expected {
			t.Fatal("live credential changed", name, err)
		}
	}
	var integrity string
	if err := s.DB.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal("live state damaged", integrity, err)
	}
	unknown := filepath.Join(second, "unrelated.txt")
	if err := os.WriteFile(unknown, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBackup(filepath.Base(second)); !errors.Is(err, ErrInvalidBackup) {
		t.Fatal("unexpected contents should be retained", err)
	}
	if _, err := ValidateBackup(second); err != nil {
		t.Fatal("rejected deletion damaged backup", err)
	}
}

func directoryLink(t *testing.T, path, target string) {
	t.Helper()
	if err := os.Symlink(target, path); err == nil {
		return
	}
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd.exe", "/c", "mklink", "/J", path, target).CombinedOutput(); err == nil {
			return
		} else {
			t.Fatalf("create test junction: %v: %s", err, out)
		}
	}
	t.Fatal("cannot create test directory link")
}

func TestDeleteBackupRefusesDirectoryLinks(t *testing.T) {
	for _, parentLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "backup-link", true: "backups-root-link"}[parentLink], func(t *testing.T) {
			s, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			outside := t.TempDir()
			sentinel := filepath.Join(outside, "state.sqlite")
			if err := os.WriteFile(sentinel, []byte("outside"), 0600); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(s.Dir, "backups")
			if parentLink {
				directoryLink(t, root, outside)
			} else {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				directoryLink(t, filepath.Join(root, "linked"), outside)
			}
			if err := s.DeleteBackup("linked"); !errors.Is(err, ErrInvalidBackup) {
				t.Fatal("directory link accepted", err)
			}
			data, err := os.ReadFile(sentinel)
			if err != nil || string(data) != "outside" {
				t.Fatal("link target changed", err)
			}
		})
	}
}
