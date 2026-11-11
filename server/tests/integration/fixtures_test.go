package integration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"dbmcp.local/server/internal/database"
	"dbmcp.local/server/internal/model"
)

func openFixture(t *testing.T, ctx context.Context, i model.Instance) *sql.DB {
	t.Helper()
	driver, dsn, err := database.DSN(i)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(75 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = db.PingContext(attempt)
		cancel()
		if err == nil {
			return db
		}
		select {
		case <-deadline.C:
			db.Close()
			t.Fatalf("container database did not become ready: %v", err)
		case <-ctx.Done():
			db.Close()
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
func fixture(t *testing.T, ctx context.Context, driver string, port int) (model.Instance, *sql.DB, string, string) {
	t.Helper()
	i := model.Instance{Name: "container fixture", Driver: driver, Enabled: true, Policy: model.DefaultPolicy(), Connection: model.Connection{Host: "127.0.0.1", Port: port, Database: "dbmcp_test", Username: "postgres", Password: "dbmcp-stage1-temporary", TLS: "disable"}}
	i.Policy.Masks = []model.MaskRule{
		{ID: "fixture-email", Target: "email", Mode: "fixed", Value: "[EMAIL]"},
		{ID: "fixture-password", Target: "password", Mode: "fixed", Value: "[REDACTED]"},
	}
	parameter, schema := "$1", "public"
	switch driver {
	case "mysql":
		i.Connection.Username = "root"
		parameter = "?"
		schema = "dbmcp_test"
	case "sqlserver":
		i.Connection.Username = "sa"
		i.Connection.Password = "Dbmcp!Test_2026Only"
		i.Connection.Database = "master"
		master := openFixture(t, ctx, i)
		if _, err := master.ExecContext(ctx, "CREATE DATABASE dbmcp_test"); err != nil {
			master.Close()
			t.Fatal(err)
		}
		master.Close()
		i.Connection.Database = "dbmcp_test"
		parameter = "@p1"
		schema = "dbo"
	case "dm":
		i.Connection.Username = "SYSDBA"
		i.Connection.Password = "Dbmcp!Test_2026Only"
		i.Connection.Schema = "SYSDBA"
		parameter = ":1"
		schema = "DBMCP_TEST"
	}
	db := openFixture(t, ctx, i)
	versionQuery := map[string]string{
		"postgres":  "SELECT version()",
		"mysql":     "SELECT version()",
		"sqlserver": "SELECT CAST(SERVERPROPERTY('ProductVersion') AS VARCHAR(100))",
		"dm":        "SELECT BANNER FROM V$VERSION WHERE ROWNUM=1",
	}[driver]
	var version string
	if err := db.QueryRowContext(ctx, versionQuery).Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("database_version=%s", version)
	table := schema + ".users"
	if driver == "dm" {
		if _, err := db.ExecContext(ctx, `CREATE USER DBMCP_TEST IDENTIFIED BY "DbmcpTest_2026Only"`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		i.Connection.Schema = "DBMCP_TEST"
	}
	for _, sql := range []string{"CREATE TABLE " + table + "(id BIGINT PRIMARY KEY,email VARCHAR(200),password VARCHAR(200),counter INTEGER)", "INSERT INTO " + table + " VALUES(1,'person@example.com','secret-value',0)", "INSERT INTO " + table + " VALUES(2,'another@example.com','secret-2',0)", "INSERT INTO " + table + " VALUES(9007199254740993,'exact@example.com','secret-3',0)"} {
		if _, err := db.ExecContext(ctx, sql); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if driver == "dm" {
		for _, q := range []string{"CREATE TABLE DBMCP_TEST.large_values(id INTEGER,text_value CLOB,binary_value BLOB)", "INSERT INTO DBMCP_TEST.large_values VALUES(1,'bounded text',HEXTORAW('00FF41'))"} {
			if _, err := db.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
	}
	switch os.Getenv("DBMCP_TEST_EXTENSIONS") {
	case "postgis":
		for _, q := range []string{"CREATE EXTENSION IF NOT EXISTS postgis", "CREATE TABLE public.locations(id BIGINT PRIMARY KEY,geom geometry(Point,4326))", "INSERT INTO public.locations VALUES(1,ST_SetSRID(ST_Point(120,30),4326))"} {
			if _, err := db.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
	case "timescaledb":
		for _, q := range []string{"CREATE EXTENSION IF NOT EXISTS timescaledb", "CREATE TABLE public.metrics(ts TIMESTAMPTZ NOT NULL,value DOUBLE PRECISION)", "SELECT create_hypertable('public.metrics','ts')", "INSERT INTO public.metrics VALUES('2026-11-09T00:00:00Z',42)"} {
			if _, err := db.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
	}
	return i, db, table, parameter
}
