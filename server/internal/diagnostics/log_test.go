package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDiagnosticCollectionRedactsAndBoundsFiles(t *testing.T) {
	dir := t.TempDir()
	logger, err := Open(dir, "server")
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 90; n++ {
		logger.Log(context.Background(), slog.LevelInfo, "test.event", "stage", strings.Repeat("x", 32000), "token", "TOKEN_CANARY")
	}
	logger.Info("raw message MESSAGE_CANARY", "password", "PASSWORD_CANARY", "error", map[string]any{"params": "PARAMS_CANARY"})
	if err = logger.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "admin.token"), []byte("SECRET_CANARY"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "server-900001.jsonl"), []byte("{\"component\":\"server\",\"msg\":\"test.event\",\"error\":{\"password\":\"PASSWORD_CANARY\"}}\nbroken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out := Collect(dir)
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "CANARY") {
		t.Fatal("diagnostic export contains secret")
	}
	if len(out.Files) < 3 {
		t.Fatal("rotation did not produce retained evidence")
	}
	var skipped int
	for _, f := range out.Files {
		skipped += f.SkippedLines
	}
	if skipped != 1 {
		t.Fatal("corrupt line not reported")
	}
	unsafe := &pgconn.PgError{Code: "23505", Message: "ROW_CANARY", Detail: "SQL_CANARY"}
	b, _ = json.Marshal(ErrorInfo(unsafe))
	if strings.Contains(string(b), "CANARY") || !strings.Contains(string(b), "23505") {
		t.Fatal("database diagnostic leaked data or lost error code")
	}
}

func TestPrunePreservesLiveProcessAndOldInactiveBudget(t *testing.T) {
	dir := t.TempDir()
	live := flock.New(filepath.Join(dir, "bridge-999999.lock"))
	if ok, e := live.TryLock(); e != nil || !ok {
		t.Fatal(e)
	}
	defer live.Unlock()
	path := filepath.Join(dir, "bridge-999999.jsonl")
	_ = os.WriteFile(path, []byte("{}\n"), 0600)
	old := time.Now().Add(-10 * 24 * time.Hour)
	_ = os.Chtimes(path, old, old)
	for n := 0; n < 40; n++ {
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("cli-%d.jsonl", n+1000)), []byte("{}\n"), 0600)
	}
	prune(dir)
	if _, e := os.Stat(path); e != nil {
		t.Fatal("live process log removed")
	}
	entries, _ := os.ReadDir(dir)
	count := 0
	for _, e := range entries {
		if logName.MatchString(e.Name()) {
			count++
		}
	}
	if count != 33 {
		t.Fatalf("retention: got %d, expected 32 inactive plus one live", count)
	}
}
