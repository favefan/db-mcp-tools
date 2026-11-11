package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"dbmcp.local/server/internal/model"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/ewkbhex"
	"github.com/twpayne/go-geom/encoding/wkt"
)

func postgresMetadata(ctx context.Context, db *sql.DB, cols []model.Column) {
	rows, err := db.QueryContext(ctx, "SELECT e.extname,n.nspname FROM pg_catalog.pg_extension e JOIN pg_catalog.pg_namespace n ON n.oid=e.extnamespace WHERE e.extname IN ('postgis','timescaledb')")
	if err != nil {
		return
	}
	extensions := map[string]string{}
	for rows.Next() {
		var name, schema string
		if rows.Scan(&name, &schema) == nil {
			extensions[name] = schema
		}
	}
	rows.Close()
	if schema, ok := extensions["postgis"]; ok {
		for _, kind := range []string{"geometry", "geography"} {
			query := "SELECT f_table_schema,f_table_name,f_" + kind + "_column,type,srid,coord_dimension FROM " + quote(schema, "postgres") + "." + quote(kind+"_columns", "postgres")
			rs, err := db.QueryContext(ctx, query)
			if err != nil {
				continue
			}
			for rs.Next() {
				var schema, table, column, spatialType string
				var srid, dimensions int
				if rs.Scan(&schema, &table, &column, &spatialType, &srid, &dimensions) != nil {
					continue
				}
				for n := range cols {
					if cols[n].Schema == schema && cols[n].Table == table && cols[n].Name == column {
						cols[n].SRID = &srid
						cols[n].Dimensions = dimensions
						cols[n].SpatialType = spatialType
					}
				}
			}
			rs.Close()
		}
	}
	if _, ok := extensions["timescaledb"]; ok {
		rs, err := db.QueryContext(ctx, "SELECT hypertable_schema,hypertable_name FROM timescaledb_information.hypertables")
		if err != nil {
			return
		}
		defer rs.Close()
		for rs.Next() {
			var schema, table string
			if rs.Scan(&schema, &table) != nil {
				continue
			}
			for n := range cols {
				if cols[n].Schema == schema && cols[n].Table == table {
					cols[n].Hypertable = true
				}
			}
		}
	}
}
func spatial(v any) (string, error) {
	var g geom.T
	var err error
	switch raw := v.(type) {
	case string:
		g, err = ewkbhex.Decode(strings.TrimPrefix(raw, `\x`))
	case []byte:
		if len(raw) > 0 && (raw[0] == 0 || raw[0] == 1) {
			g, err = ewkb.Unmarshal(raw)
		} else {
			g, err = ewkbhex.Decode(strings.TrimPrefix(string(raw), `\x`))
		}
	default:
		return "", errors.New("unsupported spatial value")
	}
	if err != nil {
		return "", errors.New("cannot decode spatial value; select ST_AsText explicitly")
	}
	text, err := wkt.Marshal(g)
	if err != nil {
		return "", errors.New("cannot encode spatial value")
	}
	if g.SRID() > 0 {
		text = fmt.Sprintf("SRID=%d;%s", g.SRID(), text)
	}
	return text, nil
}
