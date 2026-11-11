// Package runtime owns deployment, process and OS-service integration.
package runtime

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"dbmcp.local/server/internal/api"
	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/diagnostics"
	"dbmcp.local/server/internal/storage"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Admin          string   `toml:"admin" json:"admin"`
	MCP            string   `toml:"mcp" json:"mcp"`
	TLSCert        string   `toml:"tls_cert" json:"tls_cert"`
	TLSKey         string   `toml:"tls_key" json:"tls_key"`
	ReverseProxy   bool     `toml:"reverse_proxy" json:"reverse_proxy"`
	TrustedProxies []string `toml:"trusted_proxies" json:"trusted_proxies"`
}

func DefaultDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		switch runtime.GOOS {
		case "windows":
			if local := os.Getenv("LOCALAPPDATA"); local != "" {
				return filepath.Join(local, "DBMCP", "server")
			}
			return filepath.Join(home, "AppData", "Local", "DBMCP", "server")
		case "darwin":
			return filepath.Join(home, "Library", "Application Support", "DBMCP", "server")
		default:
			if data := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(data) {
				return filepath.Join(data, "dbmcp", "server")
			}
			return filepath.Join(home, ".local", "share", "dbmcp", "server")
		}
	}
	return "dbmcp-data"
}
func LoadConfig(dir string) (Config, error) {
	c := Config{Admin: "127.0.0.1:19876", MCP: "127.0.0.1:19877"}
	b, err := os.ReadFile(filepath.Join(dir, "deployment.toml"))
	if err == nil {
		if err = toml.Unmarshal(b, &c); err != nil {
			return c, errors.New("invalid deployment.toml")
		}
	} else if !os.IsNotExist(err) {
		return c, err
	}
	host, _, err := net.SplitHostPort(c.Admin)
	if err != nil {
		return c, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return c, errors.New("Admin must bind an explicit loopback IP; use an SSH tunnel for remote administration")
	}
	host, _, err = net.SplitHostPort(c.MCP)
	if err != nil {
		return c, err
	}
	ip = net.ParseIP(host)
	if (ip == nil || !ip.IsLoopback()) && (c.TLSCert == "" || c.TLSKey == "") && !c.ReverseProxy {
		return c, errors.New("remote MCP requires TLS or explicit reverse_proxy deployment")
	}
	if c.Admin == c.MCP {
		return c, errors.New("Admin and MCP require separate listeners")
	}
	if _, err = api.NewSourceResolver(c.TrustedProxies); err != nil {
		return c, errors.New("invalid trusted_proxies IP/CIDR")
	}
	return c, nil
}
func Initialize(dir string) (map[string]any, error) {
	s, err := storage.Open(dir)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	c, err := LoadConfig(dir)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(s.Dir, "deployment.toml")
	if _, err = os.Stat(p); os.IsNotExist(err) {
		b, e := toml.Marshal(c)
		if e != nil {
			return nil, e
		}
		if e = os.WriteFile(p, b, 0600); e != nil {
			return nil, e
		}
	}
	return map[string]any{"server_id": s.ServerID, "data_dir": s.Dir, "admin": c.Admin, "mcp": c.MCP, "credential_file": filepath.Join(s.Dir, "admin.token")}, nil
}
func Serve(ctx context.Context, dir, mode string) error {
	return serve(ctx, dir, mode, nil)
}
func serve(ctx context.Context, dir, mode string, ready chan<- error) (result error) {
	phase := "config.load"
	diagnostics.Event(ctx, slog.LevelInfo, "server.start", "mode", mode, "data_dir", dir, "build", diagnostics.Build())
	announced := false
	defer func() {
		level := slog.LevelInfo
		if result != nil {
			level = slog.LevelError
		}
		diagnostics.Event(ctx, level, "server.stopped", "stage", phase, "error", diagnostics.ErrorInfo(result))
		if ready != nil && !announced {
			ready <- result
		}
	}()
	c, err := LoadConfig(dir)
	if err != nil {
		return err
	}
	phase = "state.open"
	s, err := storage.Open(dir)
	if err != nil {
		return err
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	phase = "admin.listen"
	al, err := net.Listen("tcp", c.Admin)
	if err != nil {
		diagnostics.Event(ctx, slog.LevelError, "listener.failed", "stage", phase, "address", c.Admin, "error", diagnostics.ErrorInfo(err))
		return errors.New("Admin address unavailable")
	}
	defer al.Close()
	phase = "mcp.listen"
	ml, err := net.Listen("tcp", c.MCP)
	if err != nil {
		diagnostics.Event(ctx, slog.LevelError, "listener.failed", "stage", phase, "address", c.MCP, "error", diagnostics.ErrorInfo(err))
		return errors.New("MCP address unavailable")
	}
	defer ml.Close()
	if c.TLSCert != "" {
		phase = "tls.load"
		cert, err := tls.LoadX509KeyPair(c.TLSCert, c.TLSKey)
		if err != nil {
			return errors.New("cannot load MCP TLS certificate and key")
		}
		ml = tls.NewListener(ml, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}})
	}
	admin := &http.Server{Handler: api.Admin(a, cancel, mode), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	resolveSource, err := api.NewSourceResolver(c.TrustedProxies)
	if err != nil {
		return err
	}
	mcp := &http.Server{Handler: api.MCP(a, resolveSource), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	errCh := make(chan error, 2)
	go func() { errCh <- admin.Serve(al) }()
	go func() { errCh <- mcp.Serve(ml) }()
	info := map[string]any{"server_id": s.ServerID, "pid": os.Getpid(), "mode": mode, "admin": c.Admin, "mcp": c.MCP}
	phase = "runtime.write"
	b, _ := json.Marshal(info)
	if err = os.WriteFile(filepath.Join(s.Dir, "runtime.json"), b, 0600); err != nil {
		return err
	}
	defer os.Remove(filepath.Join(s.Dir, "runtime.json"))
	announced = true
	phase = "ready"
	diagnostics.Event(ctx, slog.LevelInfo, "server.ready", "server_id", s.ServerID, "mode", mode, "admin", c.Admin, "mcp", c.MCP, "schema_version", storage.SchemaVersion)
	if ready != nil {
		ready <- nil
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case e := <-errCh:
			if !errors.Is(e, http.ErrServerClosed) {
				result = e
			}
			break loop
		case <-ticker.C:
			if err := s.Cleanup(); err != nil {
				diagnostics.Event(ctx, slog.LevelError, "maintenance.cleanup_failed", "error", diagnostics.ErrorInfo(err))
			}

		}
	}
	shutdown, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()
	phase = "shutdown"
	if err := admin.Shutdown(shutdown); err != nil {
		diagnostics.Event(ctx, slog.LevelError, "shutdown.admin_failed", "error", diagnostics.ErrorInfo(err))
	}
	if err := mcp.Shutdown(shutdown); err != nil {
		diagnostics.Event(ctx, slog.LevelError, "shutdown.mcp_failed", "error", diagnostics.ErrorInfo(err))
	}
	return result
}
func AdminRequest(ctx context.Context, dir, endpoint, token, method, path string, body io.Reader) ([]byte, error) {
	if endpoint == "" {
		c, err := LoadConfig(dir)
		if err != nil {
			return nil, err
		}
		endpoint = "http://" + c.Admin
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, errors.New("invalid Admin URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return nil, errors.New("Admin requires a loopback tunnel or HTTPS")
	}
	if token == "" {
		b, err := os.ReadFile(filepath.Join(dir, "admin.token"))
		if err != nil {
			return nil, errors.New("cannot read local Admin credential; initialize data directory or set DBMCP_ADMIN_TOKEN")
		}
		token = base64.RawURLEncoding.EncodeToString(b)
	}
	if !strings.HasPrefix(path, "/admin/v1/") || strings.Contains(path, "..") {
		return nil, errors.New("invalid Admin path")
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(endpoint, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-DBMCP-Trace-ID", diagnostics.Trace(ctx))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("MCP Server is unavailable")
	}
	defer res.Body.Close()
	out, err := io.ReadAll(io.LimitReader(res.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("Admin HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Info only reads deployment configuration and probes its authenticated listener.
func Info(ctx context.Context, dir string) (map[string]any, error) {
	c, err := LoadConfig(dir)
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	status, err := Status(ctx, dir)
	if err != nil {
		return nil, err
	}
	return map[string]any{"admin": c.Admin, "mcp": c.MCP, "data_dir": absolute, "status": status}, nil
}

// Port allocation only applies to a new deployment; existing addresses are preserved.
func InitializePorts(dir string, allocate bool) (map[string]any, error) {
	if !allocate {
		return Initialize(dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "deployment.toml")); err == nil {
		return Initialize(dir)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer first.Close()
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer second.Close()
	out, err := Initialize(dir)
	if err != nil {
		return nil, err
	}
	c, err := LoadConfig(dir)
	if err != nil {
		return nil, err
	}
	c.Admin = first.Addr().String()
	c.MCP = second.Addr().String()
	bytes, err := toml.Marshal(c)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, "deployment.toml"), bytes, 0600); err != nil {
		return nil, err
	}
	out["admin"] = c.Admin
	out["mcp"] = c.MCP
	return out, nil
}
