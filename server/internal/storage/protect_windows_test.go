package storage

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExistingACLIsPreservedAndPublicGrantsRejected(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	grant := func(sid string) {
		t.Helper()
		if out, err := exec.Command("icacls.exe", dir, "/grant", sid+":(OI)(CI)M").CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	acl := func() string {
		t.Helper()
		sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		return sd.String()
	}
	grant("*S-1-5-19")
	before := acl()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if acl() != before {
		t.Fatal("opening state rewrote the service account ACL")
	}
	token := filepath.Join(dir, "admin.token")
	if out, err := exec.Command("icacls.exe", token, "/grant", "*S-1-1-0:R").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if s, err = Open(dir); err == nil {
		s.Close()
		t.Fatal("state opened with a public administrator credential file")
	}
	if out, err := exec.Command("icacls.exe", token, "/remove:g", "*S-1-1-0").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	grant("*S-1-1-0")
	if s, err = Open(dir); err == nil {
		s.Close()
		t.Fatal("state opened with an Everyone grant")
	}
}
