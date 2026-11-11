package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"dbmcp.local/server/internal/storage"
)

func prepareServiceDir(dir, username string) error {
	out, err := exec.Command("icacls.exe", dir, "/grant", username+":(OI)(CI)M", "/T", "/Q").CombinedOutput()
	if err != nil {
		return fmt.Errorf("grant service data directory: %w: %s", err, out)
	}
	runtime := runtimeDir(dir)
	if _, err = os.Stat(runtime); err == nil {
		if err = storage.Protect(runtime); err != nil {
			return err
		}
		for _, args := range [][]string{{runtime, "/remove:g", username, "/T", "/Q"}, {runtime, "/grant:r", username + ":(OI)(CI)RX", "/T", "/Q"}} {
			out, err = exec.Command("icacls.exe", args...).CombinedOutput()
			if err != nil {
				return fmt.Errorf("restrict service runtime %s: %w: %s", filepath.Base(runtime), err, out)
			}
		}
	}
	return nil
}
