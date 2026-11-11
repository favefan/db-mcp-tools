package database

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"dbmcp.local/server/internal/model"
)

func DatabaseMode(c model.Connection) string {
	if c.DatabaseMode == "" {
		return "single"
	}
	return c.DatabaseMode
}

func validDatabase(name string) bool {
	return name != "" && len(name) <= 256 && !strings.ContainsRune(name, '\x00')
}

func validateScope(c model.Connection) error {
	switch DatabaseMode(c) {
	case "single":
		if !validDatabase(c.Database) {
			return errors.New("a database name is required for single-database mode")
		}
	case "selected":
		if len(c.Databases) == 0 || len(c.Databases) > 256 {
			return errors.New("select between 1 and 256 databases")
		}
		seen := map[string]bool{}
		for _, name := range c.Databases {
			if !validDatabase(name) || seen[name] {
				return errors.New("database names must be nonempty and unique")
			}
			seen[name] = true
		}
	case "all":
		if c.Database != "" && !validDatabase(c.Database) {
			return errors.New("invalid initial database")
		}
	default:
		return errors.New("database_mode must be single, selected or all")
	}
	return nil
}

func discoveryInstance(i model.Instance) model.Instance {
	if i.Connection.Database == "" {
		switch i.Driver {
		case "postgres":
			i.Connection.Database = "postgres"
		case "sqlserver":
			i.Connection.Database = "master"
		}
	}
	return i
}

// Discover uses each engine's metadata through the existing drivers. It opens a
// short-lived control connection, so unsaved configurations never share a pool.
func (m *Manager) Discover(ctx context.Context, i model.Instance) ([]string, error) {
	if err := validateEndpoint(i); err != nil {
		return nil, err
	}
	i = discoveryInstance(i)
	driver, dsn, err := DSN(i)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, errors.New("driver unavailable")
	}
	defer db.Close()
	query := map[string]string{
		"postgres":  "SELECT datname FROM pg_catalog.pg_database WHERE datallowconn AND NOT datistemplate AND has_database_privilege(current_user,oid,'CONNECT') ORDER BY datname",
		"mysql":     "SHOW DATABASES",
		"sqlserver": "SELECT name FROM sys.databases WHERE state=0 AND HAS_DBACCESS(name)=1 ORDER BY name",
		"dm":        "SELECT NAME FROM V$DATABASE",
	}[i.Driver]
	if query == "" {
		return nil, errors.New("driver unavailable")
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		diagnosticFailure(ctx, i, "databases.discover", err)
		return nil, errors.New("database discovery failed; check account access and initial database")
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, errors.New("database list decoding failed")
		}
		if validDatabase(name) {
			out = append(out, name)
		}
		if len(out) > 10000 {
			return nil, errors.New("database list exceeds 10000 entries")
		}
	}
	if err = rows.Err(); err != nil {
		diagnosticFailure(ctx, i, "databases.stream", err)
		return nil, errors.New("database discovery interrupted")
	}
	slices.Sort(out)
	return out, nil
}

func (m *Manager) Databases(ctx context.Context, i model.Instance) ([]string, error) {
	switch DatabaseMode(i.Connection) {
	case "single":
		return []string{i.Connection.Database}, nil
	case "selected":
		return slices.Clone(i.Connection.Databases), nil
	case "all":
		return m.Discover(ctx, i)
	default:
		return nil, errors.New("invalid database scope")
	}
}

// Select resolves exactly one target before schema discovery or execution.
// Neither SQL USE statements nor cross-database references switch this target.
func (m *Manager) Select(ctx context.Context, i model.Instance, name string) (model.Instance, error) {
	mode := DatabaseMode(i.Connection)
	if name == "" {
		if mode == "single" {
			name = i.Connection.Database
		}
		if mode == "selected" && len(i.Connection.Databases) == 1 {
			name = i.Connection.Databases[0]
		}
		if mode == "all" && i.Driver == "dm" {
			names, err := m.Discover(ctx, i)
			if err != nil {
				return i, err
			}
			if len(names) == 1 {
				name = names[0]
			}
		}
	}
	if !validDatabase(name) {
		return i, errors.New("database is required; call list_databases and specify a target")
	}
	names, err := m.Databases(ctx, i)
	if err != nil {
		return i, err
	}
	if !slices.Contains(names, name) {
		return i, errors.New("database is outside this instance's configured scope")
	}
	if i.Driver == "dm" && mode == "selected" {
		actual, err := m.Discover(ctx, i)
		if err != nil {
			return i, err
		}
		if !slices.Contains(actual, name) {
			return i, errors.New("DM8 exposes one database per endpoint; use schemas for user-level scopes")
		}
	}
	i.Connection.Database = name
	return i, nil
}

// Unsaved administrative discovery gets an isolated pool and may inspect any
// account-visible database before it is added to the instance's allowed scope.
func Inspect(ctx context.Context, i model.Instance, name string) (model.Catalog, error) {
	manager := New()
	defer manager.Close()
	i.ID = model.ID()
	i.Connection.DatabaseMode = "all"
	i.Connection.Databases = nil
	i, err := manager.Select(ctx, i, name)
	if err != nil {
		return model.Catalog{}, err
	}
	return manager.Describe(ctx, i)
}

func (m *Manager) Describe(ctx context.Context, i model.Instance) (model.Catalog, error) {
	result := model.Catalog{Database: i.Connection.Database}
	var err error
	result.Columns, err = m.Catalog(ctx, i)
	if err != nil {
		return result, err
	}
	result.Schemas, err = m.Schemas(ctx, i)
	if err != nil {
		return result, err
	}
	result.Extensions, err = m.Extensions(ctx, i)
	return result, err
}

func (m *Manager) Schemas(ctx context.Context, i model.Instance) ([]string, error) {
	if i.Driver == "mysql" {
		return []string{i.Connection.Database}, nil
	}
	queries := map[string]string{
		"postgres":  "SELECT nspname FROM pg_catalog.pg_namespace WHERE nspname NOT LIKE 'pg_%' AND nspname<>'information_schema' AND has_schema_privilege(oid,'USAGE') ORDER BY nspname",
		"sqlserver": "SELECT name FROM sys.schemas WHERE name NOT IN ('sys','INFORMATION_SCHEMA') ORDER BY name",
		"dm":        "SELECT USERNAME FROM ALL_USERS ORDER BY USERNAME",
	}
	p, err := m.get(i)
	if err != nil {
		return nil, err
	}
	rows, err := p.db.QueryContext(ctx, queries[i.Driver])
	if err != nil {
		diagnosticFailure(ctx, i, "schemas.discover", err)
		return nil, errors.New("schema discovery failed")
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, errors.New("schema decoding failed")
		}
		out = append(out, name)
		if len(out) > 10000 {
			return nil, errors.New("schema list exceeds 10000 entries")
		}
	}
	return out, rows.Err()
}
