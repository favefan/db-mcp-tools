package storage

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var managementSID string

// Set once by the authorized CLI worker, before opening or creating state.
func ConfigureManagerSID(value string) error {
	if value == "" {
		managementSID = ""
		return nil
	}
	sid, err := windows.StringToSid(value)
	if err != nil {
		return err
	}
	_, _, kind, err := sid.LookupAccount("")
	if err != nil || kind != windows.SidTypeUser {
		return errors.New("management identity must be a specific user account")
	}
	managementSID = sid.String()
	return nil
}
func GrantConfiguredManager(dir string) error {
	if managementSID == "" {
		return nil
	}
	if err := Verify(dir); err != nil {
		return err
	}
	out, err := exec.Command("icacls.exe", dir, "/grant:r", "*"+managementSID+":(OI)(CI)F", "/T", "/Q").CombinedOutput()
	if err != nil {
		return fmt.Errorf("grant management user: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Restrict inherited permissions before creating any secrets. SID syntax avoids localized names.
func Protect(dir string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	extra := ""
	if managementSID != "" && managementSID != u.Uid {
		extra = "(A;OICI;FA;;;" + managementSID + ")"
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + u.Uid + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)" + extra)
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return err
	}
	return Verify(dir)
}

// Existing data directories are checked without WRITE_DAC. A service account
// deliberately receives Modify, and must not replace the administrator's ACL.
func Verify(dir string) error {
	if err := verifyACL(dir); err != nil {
		return err
	}
	for _, name := range []string{"key", "admin.token", "state.sqlite", "state.sqlite-wal", "state.sqlite-shm"} {
		path := filepath.Join(dir, name)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := verifyACL(path); err != nil {
			return fmt.Errorf("unsafe %s permissions: %w", name, err)
		}
	}
	return nil
}
func verifyACL(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if sd == nil {
		return errors.New("data directory has no security descriptor")
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if acl == nil || acl.AceCount == 0 {
		return errors.New("data directory has no private ACL")
	}
	for n := uint32(0); n < uint32(acl.AceCount); n++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(acl, n, &ace); err != nil {
			return err
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("unsupported data directory ACL; initialize a private directory")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() {
			return errors.New("invalid data directory ACL")
		}
		switch sid.String() {
		case "S-1-5-18", "S-1-5-19", "S-1-5-20", "S-1-5-32-544":
			continue
		}
		_, _, kind, err := sid.LookupAccount("")
		if err != nil || kind != windows.SidTypeUser {
			return fmt.Errorf("data directory grants access to a shared or unresolved principal %s; restrict its ACL before opening", sid.String())
		}
	}
	return nil
}
