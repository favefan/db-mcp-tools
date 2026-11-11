package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"dbmcp.local/server/internal/api"
	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
)

func objectScopes(t *testing.T, ctx context.Context, a *app.App, s *storage.Store, original model.Instance, db *sql.DB, identity model.Identity, table string) {
	t.Helper()
	i, err := s.Instance(original.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := s.Binding(identity.EntityID, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldBinding := binding
	defer func() {
		latest, err := s.Instance(i.ID, true)
		if err != nil {
			t.Error(err)
			return
		}
		original.Revision = latest.Revision
		if _, err := s.SaveInstance(original); err != nil {
			t.Error(err)
		}
		latestBinding, err := s.Binding(identity.EntityID, i.ID)
		if err != nil {
			t.Error(err)
			return
		}
		oldBinding.Revision = latestBinding.Revision
		if _, err := s.SaveBinding(oldBinding); err != nil {
			t.Error(err)
		}
		a.DB.Invalidate(i.ID)
	}()
	names, err := a.DB.Discover(ctx, i)
	if err != nil {
		t.Fatal(err)
	}
	target := i.Connection.Database
	if i.Driver == "dm" {
		target = names[0]
	}
	i.Connection.DatabaseMode = "all"
	parts := strings.Split(table, ".")
	schema, tableName := parts[0], parts[1]
	empty := "dbmcp_empty_scope"
	if i.Driver == "dm" {
		empty = "DBMCP_EMPTY_SCOPE"
	}
	if i.Driver != "mysql" {
		ddl := "CREATE SCHEMA " + empty
		if i.Driver == "dm" {
			ddl = "CREATE USER " + empty + " IDENTIFIED BY \"DbmcpTest_2026Only\""
		}
		if _, err = db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	// Draft inspection must work before any grant is selected and preserve saved secrets.
	i.Policy.Resources = []string{}
	i, err = s.SaveInstance(i)
	if err != nil {
		t.Fatal(err)
	}
	i, err = s.Instance(i.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	handler := api.Admin(a, func() {}, "test")
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(body)
		req := httptest.NewRequest(method, "/admin/v1/"+path, bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+s.AdminToken)
		req.RemoteAddr = "127.0.0.1:40000"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	draft := i
	draft.Connection.Password = ""
	response := call("POST", "instances/test", draft)
	if response.Code != 200 {
		t.Fatalf("scope-free connection: %d %s", response.Code, response.Body.String())
	}
	response = call("POST", "instances/schema", map[string]any{"instance": draft, "database": target})
	var catalog model.Catalog
	if err = json.Unmarshal(response.Body.Bytes(), &catalog); err != nil || response.Code != 200 {
		t.Fatalf("draft catalog: %d %s %v", response.Code, response.Body.String(), err)
	}
	if catalog.Database != target || len(catalog.Columns) == 0 {
		t.Fatal("wrong draft database", catalog.Database)
	}
	for _, column := range catalog.Columns {
		if strings.EqualFold(column.Schema+"."+column.Table, table) && strings.EqualFold(column.Name, "id") {
			schema, tableName = column.Schema, column.Table
		}
	}
	if i.Driver != "mysql" && !slices.Contains(catalog.Schemas, empty) {
		t.Fatal("empty schema missing", catalog.Schemas)
	}
	if i.Driver == "mysql" && (len(catalog.Schemas) != 1 || catalog.Schemas[0] != target) {
		t.Fatal("MySQL database/schema mismatch")
	}
	if response = call("GET", "instances/"+i.ID+"/schema?database="+target, nil); response.Code == 200 {
		t.Fatal("ungranted saved metadata exposed")
	}
	if response = call("POST", "instances/schema", map[string]any{"instance": draft, "database": "outside"}); response.Code == 200 {
		t.Fatal("draft inspected nonexistent database")
	}

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
	query := func(name, sqlText string) error {
		result, err := a.Query(ctx, identity, model.Query{InstanceID: i.ID, Database: name, SQL: sqlText, Tool: "db_query", ClientRequestID: model.ID()})
		if err == nil && result.Status != "succeeded" {
			t.Fatalf("unexpected query status %s", result.Status)
		}
		return err
	}
	var idColumn string
	for _, column := range catalog.Columns {
		if column.Schema == schema && column.Table == tableName && strings.EqualFold(column.Name, "id") {
			idColumn = column.Name
		}
	}
	i.Policy.Resources = []string{model.DatabaseResource(target, schema, tableName, idColumn)}
	save()
	visible, err := a.Catalog(ctx, identity, i.ID, target)
	if err != nil || len(visible) != 1 || !strings.EqualFold(visible[0].Name, "id") {
		t.Fatalf("column scope: %#v %v", visible, err)
	}
	if err = query(target, "SELECT id FROM "+table); err != nil {
		t.Fatal("permitted column", err)
	}
	if err = query(target, "SELECT email FROM "+table); err == nil {
		t.Fatal("unselected column allowed")
	}
	if i.Driver != "dm" {
		if err = query("dbmcp_second", "SELECT 1 AS id"); err == nil {
			t.Fatal("constant query escaped database scope")
		}
		otherSchema := schema
		if i.Driver == "mysql" {
			otherSchema = "dbmcp_second"
		}
		if err = query("dbmcp_second", "SELECT id FROM "+otherSchema+".users"); err == nil {
			t.Fatal("same-name table escaped database scope")
		}
	}
	visibleNames, err := a.Databases(ctx, identity, i.ID)
	if err != nil || len(visibleNames) != 1 || visibleNames[0] != target {
		t.Fatalf("qualified discovery: %v %v", visibleNames, err)
	}
	// Bootstrap database is independent from object grants.
	i.Connection.DatabaseMode = "selected"
	i.Connection.Databases = []string{target}
	i.Connection.Database = map[string]string{"postgres": "postgres", "sqlserver": "master"}[i.Driver]
	save()
	if err = query(target, "SELECT id FROM "+table); err != nil {
		t.Fatal("bootstrap changed object scope", err)
	}
	// A selected empty schema remains visible, including future descendants.
	if i.Driver != "mysql" {
		i.Policy.Resources = []string{model.DatabaseResource(target, empty) + ".*"}
		save()
		response = call("GET", "instances/"+i.ID+"/schema?database="+target, nil)
		if err = json.Unmarshal(response.Body.Bytes(), &catalog); err != nil || response.Code != 200 || len(catalog.Schemas) != 1 || catalog.Schemas[0] != empty {
			t.Fatalf("empty schema grant: %d %s", response.Code, response.Body.String())
		}
	}
	i.Policy.Resources = []string{model.DatabaseResource(target) + "/*"}
	save()
	binding.Policy.Resources = []string{model.DatabaseResource(target, schema, tableName, idColumn)}
	if binding, err = s.SaveBinding(binding); err != nil {
		t.Fatal(err)
	}
	if err = query(target, "SELECT email FROM "+table); err == nil {
		t.Fatal("binding ignored column scope")
	}
	if err = query(target, "SELECT id FROM "+table); err != nil {
		t.Fatal("binding id", err)
	}
	binding.Policy.Resources = []string{model.DatabaseResource("outside") + "/*"}
	if binding, err = s.SaveBinding(binding); err != nil {
		t.Fatal(err)
	}
	if err = query(target, "SELECT 1 AS id"); err == nil {
		t.Fatal("binding ignored database scope")
	}
}
