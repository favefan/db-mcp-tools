// Package diagnostics records bounded operational evidence without request payloads.
package diagnostics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"

	"dbmcp.local/server/internal/model"
	"github.com/gofrs/flock"
	"gopkg.in/natefinch/lumberjack.v2"
)

const MaxFileBytes = 2 << 20

type Logger struct {
	*slog.Logger
	writer *lumberjack.Logger
	lock   *flock.Flock
}

func UserDir() string {
	if runtime.GOOS == "linux" {
		base := os.Getenv("XDG_STATE_HOME")
		if !filepath.IsAbs(base) {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, "dbmcp", "logs")
	}
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Logs", "DBMCP")
	}
	base, _ := os.UserCacheDir()
	return filepath.Join(base, "DBMCP", "logs")
}

var logName = regexp.MustCompile(`^(server|cli|bridge)-[0-9]+(?:-[0-9T.\-]+)?\.jsonl$`)
var familyName = regexp.MustCompile(`^(server|cli|bridge)-[0-9]+`)
var eventName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)

func Open(dir, component string) (*Logger, error) {
	if component != "server" && component != "cli" && component != "bridge" {
		return nil, errors.New("invalid log component")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("log directory must be a regular directory")
	}
	name := fmt.Sprintf("%s-%d", component, os.Getpid())
	lock := flock.New(filepath.Join(dir, name+".lock"))
	if ok, err := lock.TryLock(); err != nil || !ok {
		return nil, errors.New("process log is already in use")
	}
	path := filepath.Join(dir, name+".jsonl")
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		_ = lock.Unlock()
		return nil, errors.New("log file must be regular")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		_ = lock.Unlock()
		return nil, err
	}
	_ = f.Close()
	prune(dir)
	w := &lumberjack.Logger{Filename: path, MaxSize: 2, MaxBackups: 3, MaxAge: 7}
	l := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
		if sensitive(attr.Key) {
			return slog.String(attr.Key, "[omitted]")
		}
		if attr.Key == slog.MessageKey && !eventName.MatchString(attr.Value.String()) {
			if strings.HasPrefix(attr.Value.String(), "http: TLS handshake error") {
				return slog.String(attr.Key, "http.tls_handshake_failed")
			}
			return slog.String(attr.Key, "unstructured."+Fingerprint(attr.Value.String()))
		}
		if attr.Value.Kind() == slog.KindAny {
			attr.Value = slog.AnyValue(Sanitize(attr.Value.Any()))
		}
		return attr
	}})).With("component", component, "pid", os.Getpid(), "version", model.Version)
	return &Logger{Logger: l, writer: w, lock: lock}, nil
}
func (l *Logger) Close() error { err := l.writer.Close(); _ = l.lock.Unlock(); return err }

// Retain 32 inactive log files. A live process owns its family lock, including
// rotated files; never unlink files that another process is still writing.
func prune(dir string) {
	entries, _ := os.ReadDir(dir)
	type file struct {
		name string
		time time.Time
	}
	all := []file{}
	for _, e := range entries {
		if logName.MatchString(e.Name()) {
			if i, err := e.Info(); err == nil && i.Mode().IsRegular() {
				all = append(all, file{e.Name(), i.ModTime()})
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].time.After(all[j].time) })
	inactive := 0
	for _, f := range all {
		lock := flock.New(filepath.Join(dir, familyName.FindString(f.name)+".lock"))
		ok, err := lock.TryLock()
		if err != nil || !ok {
			continue
		}
		inactive++
		if inactive > 32 || time.Since(f.time) > 7*24*time.Hour {
			_ = os.Remove(filepath.Join(dir, f.name))
		}
		_ = lock.Unlock()
	}
}

type traceKey struct{}

func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}
func Trace(ctx context.Context) string { id, _ := ctx.Value(traceKey{}).(string); return id }
func Event(ctx context.Context, level slog.Level, event string, attrs ...any) {
	attrs = append(attrs, "trace_id", Trace(ctx))
	attrs = append(attrs, "time_unix_ms", time.Now().UnixMilli())
	slog.Log(ctx, level, event, attrs...)
}

// ErrorInfo deliberately excludes arbitrary driver/parser messages: they may
// contain passwords, SQL literals, row values or a remote response body.
func ErrorInfo(err error) map[string]any {
	if err == nil {
		return nil
	}
	out := map[string]any{"type": fmt.Sprintf("%T", err), "fingerprint": Fingerprint(err.Error()), "kind": "application"}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		out["kind"] = "timeout"
	case errors.Is(err, context.Canceled):
		out["kind"] = "cancelled"
	case errors.Is(err, os.ErrPermission):
		out["kind"] = "permission"
	case errors.Is(err, os.ErrNotExist):
		out["kind"] = "not_found"
	}
	var network *net.OpError
	if errors.As(err, &network) {
		out["kind"] = "network"
		out["operation"] = network.Op
		out["network"] = network.Net
		if network.Addr != nil {
			out["address"] = network.Addr.String()
		}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		out["kind"] = "dns"
		out["timeout"] = dns.IsTimeout
		out["not_found"] = dns.IsNotFound
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		out["errno"] = uint64(errno)
	}
	known := map[string]string{"access denied": "access_denied", "entity registration is pending administrator authorization": "entity_pending", "revision or request conflict": "conflict", "data directory already in use": "data_directory_in_use", "invalid deployment.toml": "deployment_invalid", "state schema is newer than this server": "schema_newer", "state is missing a matching key or administrator credential; restore a complete backup": "state_incomplete", "invalid encrypted payload": "decryption_failed"}
	for text, reason := range map[string]string{"unsupported SQL; use a simple statement or an authorized view": "unsupported_sql", "exactly one SQL statement is required": "multiple_statements", "data-modifying CTEs are not supported": "write_cte", "table is not in the configured schema catalog": "table_not_in_catalog", "column source is ambiguous or unknown": "ambiguous_column", "function is outside the supported built-ins": "unsupported_function"} {
		known[text] = reason
	}
	if code := known[err.Error()]; code != "" {
		out["reason"] = code
	}
	types := []string{}
	for current := err; current != nil && len(types) < 8; current = errors.Unwrap(current) {
		types = append(types, fmt.Sprintf("%T", current))
		v := reflect.ValueOf(current)
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		if !v.IsValid() || v.Kind() != reflect.Struct {
			continue
		}
		for _, key := range []string{"Code", "Number", "SQLState", "State", "Class", "ErrCode"} {
			field := v.FieldByName(key)
			if !field.IsValid() || !field.CanInterface() {
				continue
			}
			switch field.Kind() {
			case reflect.Int, reflect.Int32, reflect.Int64, reflect.Uint16, reflect.Uint32:
				out[key] = field.Interface()
			case reflect.String:
				if code := field.String(); regexp.MustCompile(`^[A-Z0-9]{1,8}$`).MatchString(code) {
					out[key] = code
				}
			}
		}
	}
	out["cause_types"] = types
	return out
}
func Fingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func sensitive(key string) bool {
	switch strings.ToLower(key) {
	case "password", "passwd", "token", "authorization", "credential", "credentials", "secret", "key", "sql", "params", "parameters", "rows", "result", "body", "payload", "message":
		return true
	}
	return false
}
func Sanitize(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			if !sensitive(key) {
				out[key] = Sanitize(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for n, item := range v {
			out[n] = Sanitize(item)
		}
		return out
	default:
		return value
	}
}
func Build() map[string]any {
	out := map[string]any{"version": model.Version, "api_version": model.APIVersion, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, v := range info.Settings {
			if strings.HasPrefix(v.Key, "vcs.") {
				out[v.Key] = v.Value
			}
		}
	}
	return out
}
func Panic(ctx context.Context, value any) {
	Event(ctx, slog.LevelError, "process.panic", "panic_type", fmt.Sprintf("%T", value), "stack", string(debug.Stack()))
}

// CopyLimit is shared by collectors so a damaged or malicious file cannot turn
// an export into an unbounded read. Only regular, explicitly named files qualify.
func ReadRegular(path string, limit int64) ([]byte, error) {
	i, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() {
		return nil, errors.New("non-regular diagnostic file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}
