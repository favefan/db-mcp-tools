package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"slices"
	"testing"

	"dbmcp.local/server/internal/model"
)

type discoveryConnector struct {
	opened, closed int
	names          []string
}

func (c *discoveryConnector) Connect(context.Context) (driver.Conn, error) {
	c.opened++
	return &discoveryConn{c}, nil
}
func (c *discoveryConnector) Driver() driver.Driver { return nil }

type discoveryConn struct{ connector *discoveryConnector }

func (c *discoveryConn) Close() error                        { c.connector.closed++; return nil }
func (c *discoveryConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (c *discoveryConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *discoveryConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &discoveryRows{names: slices.Clone(c.connector.names)}, nil
}

type discoveryRows struct{ names []string }

func (*discoveryRows) Columns() []string { return []string{"name"} }
func (*discoveryRows) Close() error      { return nil }
func (r *discoveryRows) Next(values []driver.Value) error {
	if len(r.names) == 0 {
		return io.EOF
	}
	values[0], r.names = r.names[0], r.names[1:]
	return nil
}

func TestSavedDiscoveryReusesLoginWithoutCachingVisibility(t *testing.T) {
	i := model.Instance{ID: "saved", Driver: "sqlserver", Revision: 1, Policy: model.DefaultPolicy(), Connection: model.Connection{Host: "127.0.0.1", Port: 1433, Username: "fixture", TLS: "disable", DatabaseMode: "all"}}
	connector := &discoveryConnector{names: []string{"first"}}
	db := sql.OpenDB(connector)
	m := New()
	defer m.Close()
	m.pools[[2]string{i.ID, "master"}] = &pool{revision: i.Revision, db: db}
	for range 10 {
		names, err := m.Discover(context.Background(), i)
		if err != nil || !slices.Equal(names, connector.names) {
			t.Fatalf("discovery: %v %v", names, err)
		}
	}
	connector.names = []string{"first", "newly-visible"}
	names, err := m.Discover(context.Background(), i)
	if err != nil || !slices.Equal(names, connector.names) {
		t.Fatalf("database visibility was cached: %v %v", names, err)
	}
	if connector.opened != 1 || connector.closed != 0 {
		t.Fatalf("discovery churned logins: opened=%d closed=%d", connector.opened, connector.closed)
	}
	m.Invalidate(i.ID)
	if connector.closed != 1 {
		t.Fatal("instance invalidation retained discovery connection")
	}
}
