package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"dbmcp.local/server/internal/api"
	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The caller owns the disposable container; these databases disappear with it.
func multipleDatabases(t *testing.T, ctx context.Context, a *app.App, s *storage.Store, i model.Instance, db *sql.DB, identity model.Identity, token, table string) model.Instance {
	t.Helper()
	var err error
	i, err = s.Instance(i.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	original := i.Connection
	save := func() {
		var err error
		i, err = s.SaveInstance(i)
		if err != nil {
			t.Fatal(err)
		}
		i, err = s.Instance(i.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		a.DB.Invalidate(i.ID)
	}
	query := func(name, sqlText, id, tool string) (model.Result, error) {
		return a.Query(ctx, identity, model.Query{InstanceID: i.ID, Database: name, SQL: sqlText, ClientRequestID: id, Tool: tool})
	}
	names, err := a.DB.Discover(ctx, i)
	if err != nil {
		t.Fatal(err)
	}
	if i.Driver == "dm" {
		if len(names) != 1 {
			t.Fatalf("DM endpoint exposed unexpected databases: %v", names)
		}
		i.Connection.DatabaseMode = "all"
		save()
		result, err := query(names[0], "SELECT id FROM "+table+" WHERE id=1", "dm-all-scope", "db_query")
		if err != nil || result.Status != "succeeded" {
			t.Fatalf("DM all scope: %#v %v", result, err)
		}
		i.Connection.DatabaseMode = "selected"
		i.Connection.Databases = []string{names[0]}
		save()
		if _, err := a.Catalog(ctx, identity, i.ID, "not-this-endpoint"); err == nil {
			t.Fatal("DM scope escaped endpoint")
		}
		if _, err := a.Catalog(ctx, identity, i.ID); err != nil {
			t.Fatal("DM single selection", err)
		}
		i.Connection = original
		save()
		return i
	}
	first, second, late := original.Database, "dbmcp_second", "dbmcp_late"
	if !slices.Contains(names, first) {
		t.Fatalf("original database missing: %v", names)
	}
	if _, err = db.ExecContext(ctx, "CREATE DATABASE "+second); err != nil {
		t.Fatal(err)
	}
	other := i
	other.Connection.Database = second
	otherDB := openFixture(t, ctx, other)
	defer otherDB.Close()
	otherTable := table
	if i.Driver == "mysql" {
		otherTable = second + ".users"
	}
	if _, err = otherDB.ExecContext(ctx, "CREATE TABLE "+otherTable+"(id BIGINT PRIMARY KEY,email VARCHAR(200),counter INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err = otherDB.ExecContext(ctx, "INSERT INTO "+otherTable+" VALUES(1,'other@example.com',100)"); err != nil {
		t.Fatal(err)
	}
	i.Connection.DatabaseMode = "selected"
	i.Connection.Databases = []string{first, second}
	save()
	if i.Driver == "postgres" {
		handler := api.Admin(a, func() {}, "test")
		posted := i
		posted.Connection.Password = ""
		body, _ := json.Marshal(posted)
		req := httptest.NewRequest("POST", "/admin/v1/instances/databases", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+s.AdminToken)
		req.RemoteAddr = "127.0.0.1:40000"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		var discovered []string
		_ = json.Unmarshal(recorder.Body.Bytes(), &discovered)
		if recorder.Code != 200 || !slices.Contains(discovered, second) {
			t.Fatalf("Admin discovery with stored password: %d %s", recorder.Code, recorder.Body.String())
		}
		for _, target := range []string{second, "outside"} {
			req = httptest.NewRequest("GET", "/admin/v1/instances/"+i.ID+"/schema?database="+target, nil)
			req.Header.Set("Authorization", "Bearer "+s.AdminToken)
			req.RemoteAddr = "127.0.0.1:40000"
			recorder = httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if (target == second && recorder.Code != 200) || (target == "outside" && recorder.Code == 200) {
				t.Fatalf("Admin schema scope: %d", recorder.Code)
			}
		}
	}
	if _, err = query("", "SELECT 1 AS id", "missing-database", "db_query"); err == nil {
		t.Fatal("multi target was silently selected")
	}
	for _, name := range []string{"outside", late} {
		if _, err = a.Catalog(ctx, identity, i.ID, name); err == nil {
			t.Fatal("unselected schema exposed")
		}
	}
	for _, target := range []struct {
		name, table string
		counter     float64
	}{{first, table, 0}, {second, otherTable, 100}} {
		cols, err := a.Catalog(ctx, identity, i.ID, target.name)
		if err != nil || len(cols) == 0 {
			t.Fatal("target catalog", err)
		}
		for _, col := range cols {
			if col.Database != target.name {
				t.Fatal("catalog mixed databases")
			}
		}
		result, err := query(target.name, "SELECT counter,email FROM "+target.table+" WHERE id=1", "read-"+target.name, "db_query")
		if err != nil || result.Status != "succeeded" || len(result.Rows) != 1 {
			t.Fatalf("target query: %#v %v", result, err)
		}
		if result.Rows[0][1] != "[EMAIL]" {
			t.Fatal("multi target bypassed mandatory Mask")
		}
		encoded, _ := json.Marshal(result.Rows[0][0])
		if string(encoded) != (map[float64]string{0: "0", 100: "100"}[target.counter]) {
			t.Fatalf("wrong database result: %s", encoded)
		}
	}
	// The official MCP SDK must expose and forward the new argument as well.
	httpServer := httptest.NewServer(api.MCP(a))
	defer httpServer.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "database-scope-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp", HTTPClient: &http.Client{Transport: authTransport{token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_databases", Arguments: map[string]string{"instance_id": i.ID}})
	if err != nil || listed.IsError {
		t.Fatal("MCP database discovery", err)
	}
	encoded, _ := json.Marshal(listed.StructuredContent)
	var discovered struct {
		Databases []string `json:"databases"`
	}
	_ = json.Unmarshal(encoded, &discovered)
	if !slices.Contains(discovered.Databases, first) || !slices.Contains(discovered.Databases, second) || slices.Contains(discovered.Databases, late) {
		t.Fatalf("MCP scope %s", encoded)
	}
	described, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_tables", Arguments: map[string]string{"instance_id": i.ID, "database": second}})
	if err != nil || described.IsError {
		t.Fatalf("MCP target catalog: %#v %v", described, err)
	}
	if i.Driver == "mysql" || i.Driver == "sqlserver" {
		cross := "SELECT id FROM " + second + ".users"
		if i.Driver == "sqlserver" {
			cross = "SELECT id FROM " + second + ".dbo.users"
		}
		if _, err = query(first, cross, "cross-target-sql", "db_query"); err == nil {
			t.Fatal("SQL changed the selected database")
		}
	}
	pending, err := query(first, "UPDATE "+table+" SET counter=counter+1 WHERE id=1", "database-bound-write", "db_execute")
	if err != nil || pending.Status != "pending" {
		t.Fatalf("first approval %#v %v", pending, err)
	}
	if _, err = query(second, "UPDATE "+otherTable+" SET counter=counter+1 WHERE id=1", "database-bound-write", "db_execute"); err == nil {
		t.Fatal("request ID changed target database")
	}
	record, err := s.Request(pending.RequestID, true)
	if err != nil || record.Database != first || record.Query.Database != first {
		t.Fatal("approval target not persisted", err)
	}
	if err = a.Decide(pending.RequestID, "remember"); err != nil {
		t.Fatal(err)
	}
	result, err := a.Resume(ctx, identity, pending.RequestID)
	if err != nil || result.Status != "succeeded" {
		t.Fatal("approved target execution", err)
	}
	_, _ = a.Resume(ctx, identity, pending.RequestID)
	otherPending, err := query(second, "UPDATE "+otherTable+" SET counter=counter+1 WHERE id=1", "second-approval", "db_execute")
	if err != nil || otherPending.Status != "pending" {
		t.Fatal("remembered approval crossed database", err)
	}
	i.Connection.Databases = []string{first}
	save()
	if err = a.Decide(otherPending.RequestID, "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Resume(ctx, identity, otherPending.RequestID); err == nil {
		t.Fatal("removed database passed execution recheck")
	}
	var count int
	if err = otherDB.QueryRowContext(ctx, "SELECT counter FROM "+otherTable+" WHERE id=1").Scan(&count); err != nil || count != 100 {
		t.Fatal("wrong target was written", err)
	}
	if err = db.QueryRowContext(ctx, "SELECT counter FROM "+table+" WHERE id=1").Scan(&count); err != nil || count != 1 {
		t.Fatal("approved write repeated", err)
	}
	i.Connection.DatabaseMode = "all"
	save()
	if _, err = db.ExecContext(ctx, "CREATE DATABASE "+late); err != nil {
		t.Fatal(err)
	}
	names, err = a.Databases(ctx, identity, i.ID)
	if err != nil || !slices.Contains(names, late) {
		t.Fatalf("new database missing in all scope: %v %v", names, err)
	}
	result, err = query(late, "SELECT 7 AS id", "late-database", "db_query")
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("late database query: %#v %v", result, err)
	}
	// Restore the baseline for the rest of the centralized pipeline regression.
	if _, err = db.ExecContext(ctx, "UPDATE "+table+" SET counter=0 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	binding, err := s.Binding(identity.EntityID, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	binding.ApprovalRules = nil
	if _, err = s.SaveBinding(binding); err != nil {
		t.Fatal(err)
	}
	i.Connection = original
	save()
	return i
}
