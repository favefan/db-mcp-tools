package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func objectMetadata(t *testing.T, ctx context.Context, session *mcp.ClientSession, db *sql.DB, s *storage.Store, i model.Instance, binding model.Binding, schema string) {
	t.Helper()
	name := func(s string) string {
		if i.Driver == "dm" {
			return strings.ToUpper(s)
		}
		return s
	}
	table := schema + "." + name("metadata_table")
	view := schema + "." + name("metadata_view")
	hiddenView := schema + "." + name("metadata_hidden_view")
	identifier := func(s string) string {
		if i.Driver == "dm" {
			return `"` + name(s) + `"`
		}
		return s
	}
	fixtureTable := schema + "." + identifier("metadata_table")
	id, tenant, counter, private := identifier("id"), identifier("tenant"), identifier("counter"), identifier("private_col")
	for _, ddl := range []string{
		"CREATE TABLE " + fixtureTable + "(" + id + " INTEGER NOT NULL, " + tenant + " INTEGER NOT NULL, " + counter + " INTEGER, " + private + " INTEGER, PRIMARY KEY(" + id + "," + tenant + "))",
		"CREATE INDEX " + identifier("metadata_mixed") + " ON " + fixtureTable + "(" + counter + "," + private + ")",
		"CREATE INDEX " + identifier("metadata_visible") + " ON " + fixtureTable + "(" + id + " DESC," + counter + ")",
		"CREATE VIEW " + schema + "." + identifier("metadata_view") + " AS SELECT " + id + "," + counter + " FROM " + fixtureTable,
		"CREATE VIEW " + schema + "." + identifier("metadata_hidden_view") + " AS SELECT " + private + " FROM " + fixtureTable,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	call := func(tool, object string, out any) {
		t.Helper()
		args := map[string]string{"instance_id": i.ID, "schema": schema}
		if object != "" {
			args["table"] = name(object)
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s %s: %v %+v", tool, object, err, result)
		}
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s %s: %v", tool, data, err)
		}
	}
	var full model.TableDescription
	call("describe_table", "metadata_table", &full)
	if full.Kind != "table" || !slices.Equal(full.PrimaryKey, []string{name("id"), name("tenant")}) || len(full.Columns) != 4 || full.Columns[0].Nullable || !full.Columns[2].Nullable {
		t.Fatalf("table details: %+v", full)
	}
	visibleIndex := func(indexes []model.TableIndex) bool {
		return slices.ContainsFunc(indexes, func(index model.TableIndex) bool {
			return index.Name == name("metadata_visible") && len(index.Columns) == 2 && index.Columns[0].Name == name("id") && index.Columns[0].Descending && index.Columns[1].Name == name("counter")
		})
	}
	if !visibleIndex(full.Indexes) || !slices.ContainsFunc(full.Indexes, func(index model.TableIndex) bool { return index.Name == name("metadata_mixed") }) {
		t.Fatalf("table indexes: %+v", full.Indexes)
	}
	var views struct{ Views []string }
	call("list_views", "", &views)
	if !slices.Contains(views.Views, view) || !slices.Contains(views.Views, hiddenView) || slices.Contains(views.Views, table) {
		t.Fatalf("view list: %v", views.Views)
	}
	var viewDetails model.TableDescription
	call("describe_table", "metadata_view", &viewDetails)
	if viewDetails.Kind != "view" || len(viewDetails.Columns) != 2 || len(viewDetails.PrimaryKey) != 0 {
		t.Fatalf("view details: %+v", viewDetails)
	}

	old := binding
	binding.Policy.Resources = []string{
		model.Resource(schema, name("metadata_table"), name("id")),
		model.Resource(schema, name("metadata_table"), name("counter")),
		model.Resource(schema, name("metadata_table"), name("private_col")),
		model.Resource(schema, name("metadata_view"), name("id")),
	}
	binding.Policy.Masks = append(binding.Policy.Masks, model.MaskRule{ID: "metadata-private", Target: name("private_col"), Mode: "deny"})
	binding, err := s.SaveBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		old.Revision = binding.Revision
		if _, err := s.SaveBinding(old); err != nil {
			t.Error(err)
		}
	}()
	var limited model.TableDescription
	call("describe_table", "metadata_table", &limited)
	if len(limited.Columns) != 2 || limited.Columns[0].Name != name("id") || limited.Columns[1].Name != name("counter") || len(limited.PrimaryKey) != 0 || len(limited.Indexes) != 1 || !visibleIndex(limited.Indexes) {
		t.Fatalf("restricted table details: %+v", limited)
	}
	call("list_views", "", &views)
	if !slices.Equal(views.Views, []string{view}) {
		t.Fatalf("restricted view list: %v", views.Views)
	}
	var denied model.TableDescription
	call("describe_table", "metadata_hidden_view", &denied)
	if denied.Kind != "" || len(denied.Columns) != 0 || len(denied.Indexes) != 0 {
		t.Fatalf("hidden view details: %+v", denied)
	}
}
