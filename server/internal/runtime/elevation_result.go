package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"dbmcp.local/server/internal/storage"
)

func OpenElevationResult(path string) (*os.File, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	if filepath.Base(absolute) != "result.json" || !strings.HasPrefix(filepath.Base(parent), "dbmcp-elevation-") {
		return nil, errors.New("invalid internal authorization result path")
	}
	if err = storage.Verify(parent); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(parent, "result.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
}
