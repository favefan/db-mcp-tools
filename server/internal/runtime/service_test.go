package runtime

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceReportsReadinessAndStartupFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	occupied := listener.Addr().String()
	defer listener.Close()
	dir := t.TempDir()
	write := func(admin, mcp string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "deployment.toml"), []byte(fmt.Sprintf("admin = %q\nmcp = %q\n", admin, mcp)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(occupied, "127.0.0.1:0")
	p := &program{dir: dir}
	if err := p.Start(nil); err == nil || !strings.Contains(err.Error(), "address unavailable") {
		t.Fatalf("startup failure was reported running: %v", err)
	}
	// Two distinct listener strings allow ephemeral ports without reserving a
	// fixed port. Successful Start returns only after both listeners are bound.
	write("127.0.0.1:0", "127.0.0.2:0")
	p = &program{dir: dir}
	if err := p.Start(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime.json")); err != nil {
		t.Fatal("ready before runtime ownership was saved")
	}
	if err := p.Stop(nil); err != nil {
		t.Fatal(err)
	}
}
