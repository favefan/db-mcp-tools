//go:build !windows

package runtime

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

func prepareServiceDir(dir, username string) error {
	if username == "" {
		return nil
	}
	account, err := user.Lookup(username)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		return os.Chown(path, uid, gid)
	})
}
