package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"dbmcp.local/server/internal/storage"
)

func copyAtomic(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dest), ".dbmcp-restore-*")
	if err != nil {
		return err
	}
	name := out.Name()
	defer os.Remove(name)
	if err = out.Chmod(0600); err != nil {
		out.Close()
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err = out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	return os.Rename(name, dest)
}
func Restore(ctx context.Context, dir, backup string) (map[string]string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	backup, err = filepath.Abs(backup)
	if err != nil {
		return nil, err
	}
	if _, err = storage.ValidateBackup(backup); err != nil {
		return nil, err
	}
	// Open owns the same exclusive lock as serve. A live Server makes restore fail.
	s, err := storage.Open(dir)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	previous, err := s.Backup(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.DB.Close(); err != nil {
		return nil, err
	}
	s.DB = nil
	for _, name := range []string{"state.sqlite-wal", "state.sqlite-shm"} {
		if err = os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	for _, name := range []string{"state.sqlite", "key", "admin.token"} {
		if err = copyAtomic(filepath.Join(backup, name), filepath.Join(dir, name)); err != nil {
			for _, restore := range []string{"state.sqlite", "key", "admin.token"} {
				_ = copyAtomic(filepath.Join(previous, restore), filepath.Join(dir, restore))
			}
			return nil, fmt.Errorf("restore failed; recovery snapshot retained at %s: %w", previous, err)
		}
	}
	return map[string]string{"restored_from": backup, "previous_backup": previous, "data_dir": dir}, nil
}
