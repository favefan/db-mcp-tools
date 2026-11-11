// Package database provides pools, catalogs and bounded transaction execution.
// It deliberately has no knowledge of identities, approvals or HTTP.
package database

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dbmcp.local/server/internal/diagnostics"
	"dbmcp.local/server/internal/model"
	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
)

type pool struct {
	revision int
	db       *sql.DB
	mu       sync.Mutex
	active   int
	changed  chan struct{}
	gate     *pool
}

func diagnosticFailure(ctx context.Context, i model.Instance, stage string, err error) {
	diagnostics.Event(ctx, slog.LevelError, "database.failed", "stage", stage, "instance_id", i.ID, "driver", i.Driver, "database", i.Connection.Database, "host", i.Connection.Host, "port", i.Connection.Port, "tls", i.Connection.TLS, "error", diagnostics.ErrorInfo(err))
}

type Manager struct {
	mu    sync.Mutex
	pools map[[2]string]*pool
	gates map[string]*pool
}

func New() *Manager { return &Manager{pools: map[[2]string]*pool{}, gates: map[string]*pool{}} }
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pools {
		_ = p.db.Close()
	}
}
func (m *Manager) Invalidate(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, p := range m.pools {
		if key[0] == id {
			_ = p.db.Close()
			delete(m.pools, key)
		}
	}
	delete(m.gates, id)
}
func Validate(v model.Instance) error {
	if v.Name == "" || len(v.Name) > 200 {
		return errors.New("instance name required")
	}
	if err := validateEndpoint(v); err != nil {
		return err
	}
	if v.Driver == "dm" && DatabaseMode(v.Connection) == "selected" && len(v.Connection.Databases) > 1 {
		return errors.New("DM8 exposes one database per endpoint; select users/schemas in the resource scope instead")
	}
	return validateScope(v.Connection)
}
func ValidateConnection(v model.Instance) error { return validateEndpoint(v) }
func validateEndpoint(v model.Instance) error {
	if v.Connection.Host == "" || v.Connection.Port < 1 || v.Connection.Port > 65535 || v.Connection.Username == "" {
		return errors.New("host, port and username required")
	}
	if v.Driver != "postgres" && v.Driver != "mysql" && v.Driver != "sqlserver" && v.Driver != "dm" {
		return errors.New("unsupported database driver")
	}
	if !model.Allowed([]string{"require", "verify-full", "disable"}, v.Connection.TLS) {
		return errors.New("TLS must be require, verify-full or disable")
	}
	return nil
}
func DefaultSchema(i model.Instance) string {
	if i.Driver == "mysql" {
		return i.Connection.Database
	}
	if i.Connection.Schema != "" {
		return i.Connection.Schema
	}
	switch i.Driver {
	case "postgres":
		return "public"
	case "sqlserver":
		return "dbo"
	case "mysql":
		return i.Connection.Database
	default:
		return strings.ToUpper(i.Connection.Username)
	}
}
func DSN(v model.Instance) (string, string, error) {
	c := v.Connection
	host := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	u := url.URL{Host: host, User: url.UserPassword(c.Username, c.Password)}
	q := url.Values{}
	switch v.Driver {
	case "postgres":
		u.Scheme = "postgres"
		u.Path = "/" + c.Database
		q.Set("sslmode", c.TLS)
		q.Set("connect_timeout", "10")
		q.Set("search_path", "pg_catalog,"+quote(DefaultSchema(v), v.Driver))
		q.Set("statement_timeout", "15000")
		u.RawQuery = q.Encode()
		return "pgx", u.String(), nil
	case "mysql":
		cfg := mysql.NewConfig()
		cfg.User = c.Username
		cfg.Passwd = c.Password
		cfg.Net = "tcp"
		cfg.Addr = host
		cfg.DBName = c.Database
		cfg.ParseTime = true
		cfg.Timeout = 10 * time.Second
		cfg.ReadTimeout = 300 * time.Second
		cfg.WriteTimeout = 15 * time.Second
		cfg.MultiStatements = false
		cfg.InterpolateParams = false
		switch c.TLS {
		case "disable":
			cfg.TLSConfig = "false"
		case "require":
			cfg.TLSConfig = "skip-verify"
		default:
			cfg.TLSConfig = "true"
		}
		return "mysql", cfg.FormatDSN(), nil
	case "sqlserver":
		u.Scheme = "sqlserver"
		q.Set("database", c.Database)
		q.Set("connection timeout", "10")
		if c.TLS == "disable" {
			q.Set("encrypt", "disable")
		} else {
			q.Set("encrypt", "true")
			q.Set("TrustServerCertificate", strconv.FormatBool(c.TLS == "require"))
		}
		u.RawQuery = q.Encode()
		return "sqlserver", u.String(), nil
	case "dm":
		return dmDSN(v)
	}
	return "", "", errors.New("unsupported driver")
}
func quote(s, driver string) string {
	if driver == "mysql" {
		return "`" + strings.ReplaceAll(s, "`", "``") + "`"
	}
	if driver == "sqlserver" {
		return "[" + strings.ReplaceAll(s, "]", "]]") + "]"
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
func (m *Manager) get(i model.Instance) (*pool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := [2]string{i.ID, i.Connection.Database}
	for k, p := range m.pools {
		if k[0] == i.ID && p.revision != i.Revision {
			_ = p.db.Close()
			delete(m.pools, k)
		}
	}
	if p := m.pools[key]; p != nil {
		if p.revision == i.Revision {
			return p, nil
		}
		_ = p.db.Close()
		delete(m.pools, key)
	}
	driver, dsn, err := DSN(i)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(i.Policy.Concurrency)
	db.SetMaxIdleConns(i.Policy.Concurrency)
	db.SetConnMaxLifetime(30 * time.Minute)
	gate := m.gates[i.ID]
	if gate == nil {
		gate = &pool{changed: make(chan struct{})}
		m.gates[i.ID] = gate
	}
	p := &pool{revision: i.Revision, db: db, gate: gate}
	m.pools[key] = p
	return p, nil
}
func (m *Manager) Test(ctx context.Context, i model.Instance) error {
	return m.testConnection(ctx, discoveryInstance(i))
}
func (m *Manager) testConnection(ctx context.Context, i model.Instance) error {
	driver, dsn, err := DSN(i)
	if err != nil {
		return err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return errors.New("driver unavailable")
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		diagnosticFailure(ctx, i, "connection.ping", err)
		return errors.New("database connection failed; check endpoint, credentials and TLS")
	}
	return nil
}
func (m *Manager) Catalog(ctx context.Context, i model.Instance) ([]model.Column, error) {
	p, err := m.get(i)
	if err != nil {
		return nil, err
	}
	q := "SELECT table_schema,table_name,column_name,data_type,ordinal_position FROM information_schema.columns WHERE table_catalog="
	args := []any{i.Connection.Database}
	switch i.Driver {
	case "postgres":
		q = "SELECT table_schema,table_name,column_name,CASE WHEN data_type='USER-DEFINED' THEN udt_name ELSE data_type END,ordinal_position FROM information_schema.columns WHERE table_catalog=$1 ORDER BY table_schema,table_name,ordinal_position"
	case "mysql":
		q = "SELECT table_schema,table_name,column_name,data_type,ordinal_position FROM information_schema.columns WHERE BINARY table_schema=BINARY ? ORDER BY table_schema,table_name,ordinal_position"
	case "sqlserver":
		q += "@p1 ORDER BY table_schema,table_name,ordinal_position"
	case "dm":
		q = "SELECT OWNER,TABLE_NAME,COLUMN_NAME,DATA_TYPE,COLUMN_ID FROM ALL_TAB_COLUMNS ORDER BY OWNER,TABLE_NAME,COLUMN_ID"
		args = nil
	default:
		return nil, errors.New("driver unavailable")
	}
	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		diagnosticFailure(ctx, i, "schema.query", err)
		return nil, errors.New("schema discovery failed")
	}
	defer rows.Close()
	out := []model.Column{}
	for rows.Next() {
		var c model.Column
		c.Database = i.Connection.Database
		if err = rows.Scan(&c.Schema, &c.Table, &c.Name, &c.Type, &c.Ordinal); err != nil {
			diagnosticFailure(ctx, i, "schema.decode", err)
			return nil, errors.New("schema decoding failed")
		}
		c.DatabaseResource = model.DatabaseResource(c.Database) + "/*"
		c.Resource = model.DatabaseResource(c.Database, c.Schema, c.Table, c.Name)
		c.TableResource = model.DatabaseResource(c.Database, c.Schema, c.Table)
		c.SchemaResource = model.DatabaseResource(c.Database, c.Schema)
		out = append(out, c)
		if len(out) > 100000 {
			return nil, errors.New("schema catalog exceeds 100000 columns; restrict database account visibility")
		}
	}
	if err = rows.Err(); err != nil {
		diagnosticFailure(ctx, i, "schema.stream", err)
		return nil, errors.New("schema discovery interrupted")
	}
	rows.Close()
	if i.Driver == "postgres" {
		postgresMetadata(ctx, p.db, out)
	}
	return out, nil
}
func (m *Manager) Extensions(ctx context.Context, i model.Instance) ([]string, error) {
	if i.Driver != "postgres" {
		return []string{}, nil
	}
	p, err := m.get(i)
	if err != nil {
		return nil, err
	}
	rows, err := p.db.QueryContext(ctx, "SELECT extname || ' ' || extversion FROM pg_catalog.pg_extension WHERE extname IN ('postgis','timescaledb') ORDER BY extname")
	if err != nil {
		return nil, errors.New("extension discovery failed")
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type Transform func(int, any) (any, error)

func (p *pool) acquire(ctx context.Context, limit int) (func(), error) {
	if p.gate != nil {
		return p.gate.acquire(ctx, limit)
	}
	for {
		p.mu.Lock()
		if p.active < limit {
			p.active++
			p.mu.Unlock()
			return func() { p.mu.Lock(); p.active--; close(p.changed); p.changed = make(chan struct{}); p.mu.Unlock() }, nil
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}
func (m *Manager) Execute(ctx context.Context, i model.Instance, q model.Query, info model.Statement, maxRows, maxBytes, concurrency int, transform Transform) (model.Result, error) {
	r := model.Result{Status: "succeeded"}
	p, err := m.get(i)
	if err != nil {
		return r, err
	}
	release, err := p.acquire(ctx, concurrency)
	if err != nil {
		return r, errors.New("execution deadline exceeded")
	}
	defer release()
	opts := &sql.TxOptions{ReadOnly: info.Operation == "select" && (i.Driver == "postgres" || i.Driver == "mysql")}
	tx, err := p.db.BeginTx(ctx, opts)
	if err != nil {
		diagnosticFailure(ctx, i, "transaction.begin", err)
		return r, errors.New("cannot begin target transaction")
	}
	defer tx.Rollback()
	args := make([]any, len(q.Params))
	for n, v := range q.Params {
		if number, ok := v.(json.Number); ok {
			if integer, e := number.Int64(); e == nil {
				v = integer
			} else {
				v = number.String()
			}
		}
		if i.Driver == "sqlserver" {
			args[n] = sql.Named(fmt.Sprintf("p%d", n+1), v)
		} else {
			args[n] = v
		}
	}
	if info.ReturnsRows {
		sqlText := q.SQL
		if q.Tool == "db_explain" {
			sqlText = "EXPLAIN (FORMAT JSON, ANALYZE FALSE, VERBOSE FALSE, COSTS TRUE) " + q.SQL
		}
		rows, err := tx.QueryContext(ctx, sqlText, args...)
		if err != nil {
			diagnosticFailure(ctx, i, "statement.query", err)
			return r, errors.New("database rejected the statement")
		}
		r.Columns, err = rows.Columns()
		if err != nil {
			rows.Close()
			return r, errors.New("result metadata unavailable")
		}
		if len(r.Columns) != len(info.Outputs) {
			rows.Close()
			return r, errors.New("result columns do not match analyzed sources")
		}
		r.Rows = [][]any{}
		// Budget the complete public envelope, including the request ID assigned
		// by app and the largest truncation marker, before accumulating rows.
		envelope := r
		envelope.RequestID = strings.Repeat("0", 32)
		envelope.Truncated = true
		envelope.Reason = "byte_limit"
		envelope.Rows = [][]any{{}}
		encodedEnvelope, _ := json.Marshal(envelope)
		used := len(encodedEnvelope) - 2
		if used > maxBytes {
			rows.Close()
			return r, errors.New("column metadata exceeds the result byte limit")
		}
		for rows.Next() {
			if len(r.Rows) >= maxRows {
				r.Truncated = true
				r.Reason = "row_limit"
				break
			}
			values := make([]any, len(r.Columns))
			dest := make([]any, len(values))
			for n := range dest {
				dest[n] = &values[n]
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return r, errors.New("result decoding failed")
			}
			for n, v := range values {
				if valueBytes(v) > maxBytes {
					rows.Close()
					return r, errors.New("a result cell exceeds the byte limit; select a bounded expression")
				}
				if q.Tool == "db_explain" {
					v, err = safePlan(v)
					if err != nil {
						rows.Close()
						return r, err
					}
				}
				if v != nil && (info.Outputs[n].Type == "geometry" || info.Outputs[n].Type == "geography") {
					v, err = spatial(v)
					if err != nil {
						rows.Close()
						return r, err
					}
				}
				v, err = normalize(v, maxBytes, info.Outputs[n].Type)
				if err != nil {
					rows.Close()
					return r, err
				}
				values[n], err = transform(n, v)
				if err != nil {
					rows.Close()
					return r, err
				}
			}
			b, e := json.Marshal(values)
			if e != nil {
				rows.Close()
				return r, errors.New("result encoding failed")
			}
			if used+len(b)+1 > maxBytes {
				r.Truncated = true
				r.Reason = "byte_limit"
				break
			}
			used += len(b) + 1
			r.Rows = append(r.Rows, values)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil || closeErr != nil {
			diagnosticFailure(ctx, i, "result.stream", errors.Join(err, closeErr))
			return r, errors.New("result stream interrupted")
		}
	} else {
		res, err := tx.ExecContext(ctx, q.SQL, args...)
		if err != nil {
			diagnosticFailure(ctx, i, "statement.execute", err)
			return r, errors.New("database rejected the statement")
		}
		r.Affected, _ = res.RowsAffected()
	}
	if err = tx.Commit(); err != nil {
		diagnosticFailure(ctx, i, "transaction.commit_unknown", err)
		r.Status = "result_unknown"
		r.Reason = "commit outcome is unknown; inspect target database before retrying"
		return r, nil
	}
	return r, nil
}
func valueBytes(v any) int {
	switch x := v.(type) {
	case string:
		return len(x)
	case []byte:
		return len(x)
	default:
		return 0
	}
}
func normalize(v any, limit int, typ string) (any, error) {
	if x, handled, err := nativeValue(v, limit); handled {
		return x, err
	}
	switch t := v.(type) {
	case []byte:
		if !utf8.Valid(t) || slices.Contains([]string{"bytea", "blob", "binary", "varbinary", "image", "bit"}, strings.ToLower(typ)) {
			return "base64:" + base64.StdEncoding.EncodeToString(t), nil
		}
		return string(t), nil
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano), nil
	case int64:
		if t > 9007199254740991 || t < -9007199254740991 {
			return strconv.FormatInt(t, 10), nil
		}
		return t, nil
	case nil, string, bool, float64:
		return v, nil
	case fmt.Stringer:
		return t.String(), nil
	default:
		return nil, errors.New("unsupported database value type")
	}
}
