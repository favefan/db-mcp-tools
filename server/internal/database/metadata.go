package database

import (
	"context"
	"database/sql"
	"errors"

	"dbmcp.local/server/internal/model"
)

// ViewNames returns ordinary views from the target database's catalog.
// The caller must intersect these names with authorized columns before returning them.
func (m *Manager) ViewNames(ctx context.Context, i model.Instance, schema, table string) (map[[2]string]bool, error) {
	p, err := m.get(i)
	if err != nil {
		return nil, err
	}
	var q, bySchema, byTable string
	args := []any{i.Connection.Database}
	switch i.Driver {
	case "postgres":
		q = `SELECT table_schema,table_name FROM information_schema.tables WHERE table_catalog=$1 AND table_type='VIEW'`
		bySchema, byTable = ` AND table_schema=$2`, ` AND table_name=$3`
	case "mysql":
		// TABLES uses the same object visibility as the column catalog.
		q = `SELECT table_schema,table_name FROM information_schema.tables WHERE BINARY table_schema=BINARY ? AND table_type='VIEW'`
		bySchema, byTable = ` AND BINARY table_schema=BINARY ?`, ` AND BINARY table_name=BINARY ?`
	case "sqlserver":
		q = `SELECT table_schema,table_name FROM information_schema.tables WHERE table_catalog=@p1 AND table_type='VIEW'`
		bySchema, byTable = ` AND table_schema=@p2`, ` AND table_name=@p3`
	case "dm":
		q = `SELECT OWNER,VIEW_NAME FROM SYS.ALL_VIEWS WHERE 1=1`
		args = nil
		bySchema, byTable = ` AND OWNER=:1`, ` AND VIEW_NAME=:2`
	default:
		return nil, errors.New("driver unavailable")
	}
	if schema != "" {
		q += bySchema
		args = append(args, schema)
	}
	if table != "" {
		q += byTable
		args = append(args, table)
	}
	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, failure(ctx, i, "schema.views.query", "view discovery failed", err)
	}
	defer rows.Close()
	out := map[[2]string]bool{}
	for rows.Next() {
		var name [2]string
		if err = rows.Scan(&name[0], &name[1]); err != nil {
			return nil, failure(ctx, i, "schema.views.decode", "view discovery failed", err)
		}
		out[name] = true
	}
	if err = rows.Err(); err != nil {
		return nil, failure(ctx, i, "schema.views.stream", "view discovery failed", err)
	}
	return out, nil
}

// TableIndexes loads complete ordered index components before policy filtering.
// DM reports its primary constraint separately from the backing index.
func (m *Manager) TableIndexes(ctx context.Context, i model.Instance, schema, table string) ([]model.TableIndex, []string, error) {
	p, err := m.get(i)
	if err != nil {
		return nil, nil, err
	}
	query := indexQueries[i.Driver]
	if query == "" {
		return nil, nil, errors.New("driver unavailable")
	}
	rows, err := p.db.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, nil, failure(ctx, i, "schema.indexes.query", "index discovery failed", err)
	}
	defer rows.Close()
	out := []model.TableIndex{}
	var primary []string
	positions := map[string]int{}
	for rows.Next() {
		var owner, object string
		var idx model.TableIndex
		var col model.IndexColumn
		var column sql.NullString
		var primaryIndex bool
		if err = rows.Scan(&owner, &object, &idx.Name, &idx.Type, &idx.Unique, &primaryIndex, &column, &col.Descending, &col.Included, &col.Prefix); err != nil {
			return nil, nil, failure(ctx, i, "schema.indexes.decode", "index discovery failed", err)
		}
		if owner != schema || object != table {
			continue
		}
		col.Name = column.String
		if primaryIndex && !col.Included {
			primary = append(primary, col.Name)
		}
		pos, exists := positions[idx.Name]
		if !exists {
			pos = len(out)
			positions[idx.Name] = pos
			out = append(out, idx)
		}
		out[pos].Columns = append(out[pos].Columns, col)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, failure(ctx, i, "schema.indexes.stream", "index discovery failed", err)
	}
	if i.Driver == "dm" {
		rows.Close()
		primary, err = dmPrimaryKey(ctx, p.db, i, schema, table)
		if err != nil {
			return nil, nil, err
		}
	}
	return out, primary, nil
}

func dmPrimaryKey(ctx context.Context, db *sql.DB, i model.Instance, schema, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT c.OWNER,c.TABLE_NAME,c.COLUMN_NAME
		FROM SYS.ALL_CONSTRAINTS k
		JOIN SYS.ALL_CONS_COLUMNS c ON c.OWNER=k.OWNER AND c.TABLE_NAME=k.TABLE_NAME AND c.CONSTRAINT_NAME=k.CONSTRAINT_NAME
		WHERE k.OWNER=:1 AND k.TABLE_NAME=:2 AND k.CONSTRAINT_TYPE='P'
		ORDER BY c.POSITION`, schema, table)
	if err != nil {
		return nil, failure(ctx, i, "schema.primary_key.query", "primary key discovery failed", err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var owner, object, column string
		if err = rows.Scan(&owner, &object, &column); err != nil {
			return nil, failure(ctx, i, "schema.primary_key.decode", "primary key discovery failed", err)
		}
		if owner == schema && object == table {
			columns = append(columns, column)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, failure(ctx, i, "schema.primary_key.stream", "primary key discovery failed", err)
	}
	return columns, nil
}

var indexQueries = map[string]string{
	"postgres": `SELECT ns.nspname,t.relname,ix.relname,am.amname,k.indisunique,k.indisprimary,
		COALESCE(c.attname,''),COALESCE((k.indoption[p.pos::int-1] & 1) <> 0,false),p.pos > k.indnkeyatts,0
		FROM pg_catalog.pg_index k
		JOIN pg_catalog.pg_class t ON t.oid=k.indrelid
		JOIN pg_catalog.pg_namespace ns ON ns.oid=t.relnamespace
		JOIN pg_catalog.pg_class ix ON ix.oid=k.indexrelid
		JOIN pg_catalog.pg_am am ON am.oid=ix.relam
		CROSS JOIN LATERAL unnest(k.indkey) WITH ORDINALITY p(attnum,pos)
		LEFT JOIN pg_catalog.pg_attribute c ON c.attrelid=t.oid AND c.attnum=p.attnum AND NOT c.attisdropped
		WHERE ns.nspname=$1 AND t.relname=$2 AND k.indisvalid AND k.indpred IS NULL AND k.indexprs IS NULL
		ORDER BY ix.relname,p.pos`,
	"mysql": `SELECT TABLE_SCHEMA,TABLE_NAME,INDEX_NAME,INDEX_TYPE,NON_UNIQUE=0,INDEX_NAME='PRIMARY',
		COALESCE(COLUMN_NAME,''),COALESCE(COLLATION='D',0),0,COALESCE(SUB_PART,0)
		FROM information_schema.STATISTICS WHERE BINARY TABLE_SCHEMA=BINARY ? AND BINARY TABLE_NAME=BINARY ?
		ORDER BY INDEX_NAME,SEQ_IN_INDEX`,
	"sqlserver": `SELECT s.name,o.name,i.name,i.type_desc,i.is_unique,i.is_primary_key,
		COALESCE(c.name,''),k.is_descending_key,k.is_included_column,0
		FROM sys.indexes i
		JOIN sys.objects o ON o.object_id=i.object_id
		JOIN sys.schemas s ON s.schema_id=o.schema_id
		JOIN sys.index_columns k ON k.object_id=i.object_id AND k.index_id=i.index_id
		LEFT JOIN sys.columns c ON c.object_id=k.object_id AND c.column_id=k.column_id
		WHERE s.name=@p1 AND o.name=@p2 AND i.type IN (1,2) AND i.is_hypothetical=0 AND i.has_filter=0
		ORDER BY i.name,CASE WHEN k.key_ordinal=0 THEN 1 ELSE 0 END,k.key_ordinal,k.index_column_id`,
	"dm": `SELECT i.TABLE_OWNER,i.TABLE_NAME,i.INDEX_NAME,i.INDEX_TYPE,
		CASE WHEN i.UNIQUENESS='UNIQUE' THEN 1 ELSE 0 END,0,
		c.COLUMN_NAME,CASE WHEN c.DESCEND='DESC' THEN 1 ELSE 0 END,0,0
		FROM SYS.ALL_INDEXES i
		JOIN SYS.ALL_IND_COLUMNS c ON c.INDEX_OWNER=i.OWNER AND c.INDEX_NAME=i.INDEX_NAME
			AND c.TABLE_OWNER=i.TABLE_OWNER AND c.TABLE_NAME=i.TABLE_NAME
		WHERE i.TABLE_OWNER=:1 AND i.TABLE_NAME=:2
		ORDER BY i.INDEX_NAME,c.COLUMN_POSITION`,
}
