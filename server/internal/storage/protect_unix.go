//go:build !windows

package storage

import (
	"errors"
	"os"
)

func Protect(dir string) error { return os.Chmod(dir, 0700) }
func ConfigureManagerSID(string) error {
	return errors.New("Windows management SID is not supported on this platform")
}
func GrantConfiguredManager(string) error { return nil }

func Verify(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("data directory must be private (chmod 700)")
	}
	return nil
}
