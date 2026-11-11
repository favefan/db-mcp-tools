package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"dbmcp.local/server/internal/storage"
	"golang.org/x/sys/windows"
)

// SHELLEXECUTEINFO is a direct binding to the Windows shell, using runas so
// Windows owns the authorization prompt. No shell or script interprets arguments.
type shellExecuteInfo struct {
	Size, Mask                        uint32
	Window                            windows.Handle
	Verb, File, Parameters, Directory *uint16
	Show                              int32
	Instance                          windows.Handle
	IDList                            unsafe.Pointer
	Class                             *uint16
	ClassKey                          windows.Handle
	HotKey                            uint32
	Icon, Process                     windows.Handle
}

func Elevate(ctx context.Context, args []string) (json.RawMessage, error) {
	dir, err := os.MkdirTemp("", "dbmcp-elevation-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err = storage.Protect(dir); err != nil {
		return nil, err
	}
	result := filepath.Join(dir, "result.json")
	manager, err := user.Current()
	if err != nil {
		return nil, err
	}
	args = append(args, "--result-file", result, "--manager-sid", manager.Uid)
	escaped := make([]string, len(args))
	for n, arg := range args {
		escaped[n] = syscall.EscapeArg(arg)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return nil, err
	}
	params, err := windows.UTF16PtrFromString(strings.Join(escaped, " "))
	if err != nil {
		return nil, err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	info := shellExecuteInfo{Mask: 0x40 | 0x100, Verb: verb, File: file, Parameters: params, Show: 0}
	info.Size = uint32(unsafe.Sizeof(info))
	execute := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")
	ok, _, callErr := execute.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return nil, callErr
	}
	defer windows.CloseHandle(info.Process)
	for {
		waited, err := windows.WaitForSingleObject(info.Process, 250)
		if err != nil {
			return nil, err
		}
		if waited == windows.WAIT_OBJECT_0 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("authorization wait interrupted; check the native service state before retrying")
		default:
		}
	}
	bytes, err := os.ReadFile(result)
	if err != nil {
		return nil, errors.New("authorized command returned no result; check its selected program and native service state")
	}
	var response map[string]json.RawMessage
	if err = json.Unmarshal(bytes, &response); err != nil {
		return nil, errors.New("authorized command returned invalid JSON")
	}
	if failure := response["error"]; failure != nil {
		var message string
		if json.Unmarshal(failure, &message) == nil {
			return nil, errors.New(message)
		}
	}
	return bytes, nil
}
