package app

import (
	"context"
	"errors"
	"slices"

	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/sqlguard"
)

func (a *App) ListViews(ctx context.Context, id model.Identity, instanceID, database, schema string) ([]string, error) {
	ctx, cancel, i, b, err := a.catalogTarget(ctx, id, instanceID, database)
	if err != nil {
		return nil, err
	}
	defer cancel()
	cols, err := a.DB.Catalog(ctx, i)
	if err != nil {
		return nil, err
	}
	views, err := a.DB.ViewNames(ctx, i, schema, "")
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, c := range VisibleColumns(cols, i.Policy, b.Policy) {
		key := [2]string{c.Schema, c.Table}
		if (schema == "" || c.Schema == schema) && views[key] {
			out = append(out, c.Schema+"."+c.Table)
			delete(views, key)
		}
	}
	return out, nil
}

func (a *App) DescribeTable(ctx context.Context, id model.Identity, instanceID, database, schema, table string) (model.TableDescription, error) {
	out := model.TableDescription{Columns: []model.TableColumn{}, PrimaryKey: []string{}, Indexes: []model.TableIndex{}}
	if schema == "" || table == "" {
		return out, errors.New("schema and table are required")
	}
	ctx, cancel, i, b, err := a.catalogTarget(ctx, id, instanceID, database)
	if err != nil {
		return out, err
	}
	defer cancel()
	if sqlguard.SystemSchema(schema) || !i.Policy.AllowsTable(i.Connection.Database, schema, table) || !b.Policy.AllowsTable(i.Connection.Database, schema, table) {
		return out, nil
	}
	cols, nullable, err := a.DB.TableColumns(ctx, i, schema, table)
	if err != nil {
		return out, err
	}
	visible := map[string]bool{}
	for _, c := range VisibleColumns(cols, i.Policy, b.Policy) {
		visible[c.Name] = true
		out.Columns = append(out.Columns, model.TableColumn{Column: c, Nullable: nullable[c.Name]})
	}
	if len(visible) == 0 {
		return out, nil
	}
	views, err := a.DB.ViewNames(ctx, i, schema, table)
	if err != nil {
		return out, err
	}
	out.Kind = "table"
	if views[[2]string{schema, table}] {
		out.Kind = "view"
		if i.Driver != "sqlserver" {
			return out, nil
		}
	}
	indexes, primary, err := a.DB.TableIndexes(ctx, i, schema, table)
	if err != nil {
		return out, err
	}
	for _, idx := range indexes {
		if !slices.ContainsFunc(idx.Columns, func(c model.IndexColumn) bool { return !visible[c.Name] }) {
			out.Indexes = append(out.Indexes, idx)
		}
	}
	if len(primary) > 0 && !slices.ContainsFunc(primary, func(name string) bool { return !visible[name] }) {
		out.PrimaryKey = primary
	}
	return out, nil
}
