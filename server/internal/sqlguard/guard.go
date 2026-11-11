// Package sqlguard adapts the four pinned Omni ASTs to a small, fail-closed
// authorization model. Reflection is confined to this adapter: only explicitly
// supported AST node kinds and fields can reach the resolver.
package sqlguard

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"dbmcp.local/server/internal/model"
	"github.com/bytebase/omni/mssql"
	mysqlparser "github.com/bytebase/omni/mysql/parser"
	"github.com/bytebase/omni/oracle"
	"github.com/bytebase/omni/pg"
)

var ErrUnsupported = errors.New("unsupported SQL; use a simple statement or an authorized view")

func SystemSchema(s string) bool {
	return slices.Contains([]string{"pg_catalog", "information_schema", "sys", "system"}, strings.ToLower(s))
}

type analyzer struct {
	driver, schema, database string
	catalog                  []model.Column
	info                     model.Statement
	depth                    int
}
type relation struct {
	alias, schema, table string
	columns              []model.Output
}
type scope struct {
	relations []relation
	outer     *scope
	ctes      map[string][]model.Output
	aliases   map[string]model.Output
}

func Analyze(driver, database, schema, sql string, catalog []model.Column) (out model.Statement, err error) {
	// Parser bugs must become a rejected request, never a server crash or a fallback.
	defer func() {
		if recover() != nil {
			out = model.Statement{}
			err = ErrUnsupported
		}
	}()
	if len(sql) == 0 || len(sql) > 128<<10 {
		return out, ErrUnsupported
	}
	var nodes []any
	switch driver {
	case "postgres":
		ss, e := pg.Parse(sql)
		if e != nil {
			return out, ErrUnsupported
		}
		for _, s := range ss {
			if s.AST != nil {
				nodes = append(nodes, s.AST)
			}
		}
	case "mysql":
		ss, e := mysqlparser.Parse(sql)
		if e != nil {
			return out, ErrUnsupported
		}
		if ss != nil {
			for _, s := range ss.Items {
				if kind(s) == "RawStmt" {
					nodes = append(nodes, first(s, "Stmt"))
				} else {
					nodes = append(nodes, s)
				}
			}
		}
	case "sqlserver":
		ss, e := mssql.Parse(sql)
		if e != nil {
			return out, ErrUnsupported
		}
		for _, s := range ss {
			if s.AST != nil {
				nodes = append(nodes, s.AST)
			}
		}
	case "dm":
		ss, e := oracle.Parse(sql)
		if e != nil {
			return out, ErrUnsupported
		}
		for _, s := range ss {
			if s.AST != nil {
				nodes = append(nodes, s.AST)
			}
		}
	default:
		return out, ErrUnsupported
	}
	if len(nodes) != 1 {
		return out, fmt.Errorf("exactly one SQL statement is required")
	}
	a := &analyzer{driver: driver, database: database, schema: schema, catalog: catalog}
	a.info.Operation = operation(nodes[0])
	if a.info.Operation == "" {
		return out, ErrUnsupported
	}
	a.info.Outputs, err = a.statement(nodes[0], nil, nil)
	if err != nil {
		return out, err
	}
	a.info.ReturnsRows = a.info.Operation == "select" || len(a.info.Outputs) > 0
	return a.info, nil
}
func operation(n any) string {
	switch kind(n) {
	case "SelectStmt":
		return "select"
	case "InsertStmt":
		return "insert"
	case "UpdateStmt":
		return "update"
	case "DeleteStmt":
		return "delete"
	}
	return ""
}
func value(n any) reflect.Value {
	v := reflect.ValueOf(n)
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}
func kind(n any) string {
	v := value(n)
	if !v.IsValid() {
		return ""
	}
	return v.Type().Name()
}
func field(n any, name string) any {
	v := value(n)
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return nil
	}
	f := v.FieldByName(name)
	if !f.IsValid() || !f.CanInterface() {
		return nil
	}
	if f.IsZero() {
		return nil
	}
	return f.Interface()
}
func first(n any, names ...string) any {
	for _, s := range names {
		if f := field(n, s); f != nil {
			return f
		}
	}
	return nil
}
func text(n any, names ...string) string {
	for _, s := range names {
		f := field(n, s)
		if v, ok := f.(string); ok {
			return v
		}
	}
	return ""
}
func present(n any, names ...string) bool { return first(n, names...) != nil }
func items(n any) []any {
	v := value(n)
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Struct {
		if f := field(n, "Items"); f != nil {
			return items(f)
		}
		return []any{n}
	}
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return []any{n}
	}
	out := []any{}
	for i := 0; i < v.Len(); i++ {
		out = append(out, v.Index(i).Interface())
	}
	return out
}
func alias(n any) string {
	f := field(n, "Alias")
	if s, ok := f.(string); ok {
		return s
	}
	return text(f, "Aliasname", "Name")
}
func stringsIn(n any) []string {
	out := []string{}
	for _, v := range items(n) {
		if s, ok := v.(string); ok {
			out = append(out, s)
		} else if kind(v) == "A_Star" {
			out = append(out, "*")
		} else {
			out = append(out, text(v, "Sval", "Str", "Name", "Column"))
		}
	}
	return out
}
func cloneCTEs(v map[string][]model.Output) map[string][]model.Output {
	out := map[string][]model.Output{}
	for k, x := range v {
		out[k] = x
	}
	return out
}
func (a *analyzer) equal(x, y string) bool {
	if a.driver == "postgres" {
		return x == y
	}
	return strings.EqualFold(x, y)
}

func (a *analyzer) statement(n any, parent *scope, ctes map[string][]model.Output) ([]model.Output, error) {
	a.depth++
	defer func() { a.depth-- }()
	if a.depth > 24 {
		return nil, ErrUnsupported
	}
	op := operation(n)
	if op == "" {
		return nil, ErrUnsupported
	}
	if present(n, "IntoClause", "IntoTable", "Into", "IntoVars", "ForUpdate", "LockingClause", "OnConflictClause", "OnDuplicateKey", "IsReplace", "ErrorLog", "MultiTable", "Dblink", "DBLink", "Hierarchical", "ModelClause", "ForClause", "OptionClause", "Hints", "ValuesRecord", "ByName", "ByPosition") {
		return nil, ErrUnsupported
	}
	s := &scope{outer: parent, ctes: cloneCTEs(ctes), aliases: map[string]model.Output{}}
	w := first(n, "WithClause")
	if present(w, "Recursive") {
		return nil, ErrUnsupported
	}
	for _, c := range items(first(w, "Ctes", "CTEs")) {
		if c == nil {
			continue
		}
		if err := a.cte(s, c); err != nil {
			return nil, err
		}
	}
	for _, c := range items(field(n, "CTEs")) {
		if err := a.cte(s, c); err != nil {
			return nil, err
		}
	}
	left, right := first(n, "Larg", "Left"), first(n, "Rarg", "Right")
	if left != nil || right != nil {
		if left == nil || right == nil {
			return nil, ErrUnsupported
		}
		l, e := a.statement(left, parent, s.ctes)
		if e != nil {
			return nil, e
		}
		r, e := a.statement(right, parent, s.ctes)
		if e != nil || len(l) != len(r) {
			return nil, ErrUnsupported
		}
		for i := range l {
			l[i].Sources = unique(append(l[i].Sources, r[i].Sources...))
			l[i].Derived = true
		}
		return l, nil
	}
	if p := field(n, "ParenSource"); p != nil {
		return a.statement(p, parent, s.ctes)
	}
	if op != "select" {
		target := first(n, "Relation", "Table", "Target")
		if target == nil {
			target = first(n, "Tables")
		}
		if err := a.from(s, target); err != nil {
			return nil, err
		}
		if len(s.relations) != 1 {
			return nil, ErrUnsupported
		}
		if s.relations[0].table == "" {
			return nil, ErrUnsupported
		}
		// OUTPUT inserted/deleted resolves to the same authorized target columns.
		if a.driver == "sqlserver" {
			r := s.relations[0]
			r.alias = "inserted"
			s.relations = append(s.relations, r)
			r.alias = "deleted"
			s.relations = append(s.relations, r)
		}
	}
	for _, f := range []string{"FromClause", "From"} {
		if err := a.from(s, field(n, f)); err != nil {
			return nil, err
		}
	}
	if op == "select" && present(n, "Tables") {
		return nil, ErrUnsupported
	}
	if present(n, "Using") {
		return nil, ErrUnsupported
	}
	if op == "insert" {
		cols := first(n, "Cols", "Columns")
		if cols == nil {
			for _, c := range s.relations[0].columns {
				a.info.Reads = unique(append(a.info.Reads, c.Sources...))
			}
		} else {
			for _, v := range items(cols) {
				name := text(v, "Name", "Column", "Str", "Sval")
				if _, err := a.resolve(s, "", "", name, false); err != nil {
					return nil, err
				}
			}
		}
		if q := first(n, "SelectStmt", "Select", "Source"); q != nil {
			if operation(q) == "select" {
				if _, err := a.statement(q, parent, s.ctes); err != nil {
					return nil, err
				}
			} else if _, err := a.expr(s, q, false); err != nil {
				return nil, err
			}
		}
		for _, f := range []string{"Values", "SetList", "SetClauses"} {
			if _, err := a.expr(s, field(n, f), false); err != nil {
				return nil, err
			}
		}
	}
	if op == "update" {
		for _, v := range items(first(n, "TargetList", "SetClause", "SetList", "SetClauses")) {
			if present(v, "Variable", "VarColumn", "WriteMethod", "Indirection", "Columns") {
				return nil, ErrUnsupported
			}
			if col := field(v, "Column"); col != nil {
				if _, err := a.expr(s, col, false); err != nil {
					return nil, err
				}
			} else {
				if _, err := a.resolve(s, "", "", text(v, "Name"), false); err != nil {
					return nil, err
				}
			}
			if _, err := a.expr(s, first(v, "Val", "Value"), false); err != nil {
				return nil, err
			}
		}
	}
	var targets any
	if op == "select" {
		targets = field(n, "TargetList")
	} else {
		targets = first(n, "ReturningList", "Returning")
		if o := field(n, "OutputClause"); o != nil {
			if present(o, "IntoTable", "IntoCols") {
				return nil, ErrUnsupported
			}
			targets = field(o, "Targets")
		}
	}
	outputs := []model.Output{}
	for _, t := range items(targets) {
		expr := first(t, "Val", "Expr")
		if expr == nil {
			expr = t
		}
		name := text(t, "Name")
		sch, table, col, isref := a.column(expr)
		if isref && col == "*" {
			expanded, err := a.star(s, sch, table)
			if err != nil {
				return nil, err
			}
			outputs = append(outputs, expanded...)
			continue
		}
		sources, err := a.expr(s, expr, false)
		if err != nil {
			return nil, err
		}
		if name == "" && isref {
			name = col
		}
		derived := !isref
		outputType := ""
		if isref {
			for _, rel := range s.relations {
				for _, c := range rel.columns {
					if a.equal(c.Name, col) && (table == "" || a.equal(table, rel.alias) || a.equal(table, rel.table)) {
						derived = derived || c.Derived
						outputType = c.Type
					}
				}
			}
		}
		o := model.Output{Name: name, Sources: sources, Derived: derived, Type: outputType}
		outputs = append(outputs, o)
		if name != "" {
			s.aliases[name] = o
		}
	}
	for _, f := range []string{"WhereClause", "Where", "GroupClause", "GroupByClause", "GroupBy", "HavingClause", "Having", "SortClause", "OrderByClause", "OrderBy", "WindowClause", "WindowDefs", "LimitOffset", "LimitCount", "Limit", "Top", "OffsetClause", "FetchClause", "FetchFirst", "QualifyClause", "ValuesLists"} {
		if _, err := a.expr(s, field(n, f), f == "SortClause" || f == "OrderBy" || f == "OrderByClause"); err != nil {
			return nil, err
		}
	}
	return outputs, nil
}
func (a *analyzer) cte(s *scope, c any) error {
	if present(c, "Recursive", "Cterecursive", "SearchClause", "CycleClause", "Search", "Cycle") {
		return ErrUnsupported
	}
	q := first(c, "Ctequery", "Query", "Select")
	if operation(q) != "select" {
		return fmt.Errorf("data-modifying CTEs are not supported")
	}
	o, err := a.statement(q, nil, s.ctes)
	if err != nil {
		return err
	}
	names := stringsIn(first(c, "Aliascolnames", "Columns"))
	if len(names) > 0 {
		if len(names) != len(o) {
			return ErrUnsupported
		}
		for i := range names {
			o[i].Name = names[i]
		}
	}
	name := text(c, "Ctename", "Name")
	if name == "" {
		return ErrUnsupported
	}
	s.ctes[name] = o
	return nil
}
func (a *analyzer) from(s *scope, n any) error {
	for _, t := range items(n) {
		switch kind(t) {
		case "RangeVar", "TableRef", "ObjectName":
			if present(field(t, "Alias"), "Colnames", "Cols") {
				return ErrUnsupported
			}
			if catalog := text(t, "Catalogname", "Database"); catalog != "" && catalog != a.database {
				return ErrUnsupported
			}
			if present(t, "Server", "DBLink", "Dblink", "Hints", "Temporal", "Sample", "Flashback", "PartitionExt") {
				return ErrUnsupported
			}
			sch, table := text(t, "Schemaname", "Schema"), text(t, "Relname", "Object", "Name")
			if a.driver == "mysql" && sch != "" && sch != a.database {
				return ErrUnsupported
			}
			if a.driver == "dm" && kind(t) == "TableRef" {
				name := field(t, "Name")
				sch, table = text(name, "Schema"), text(name, "Name")
				if present(name, "DBLink") {
					return ErrUnsupported
				}
			}
			if table == "" {
				return ErrUnsupported
			}
			al := alias(t)
			if al == "" {
				al = table
			}
			if sch == "" {
				if o, ok := s.ctes[table]; ok {
					s.relations = append(s.relations, relation{alias: al, columns: o})
					continue
				}
				// Built-ins precede the configured schema in search_path. Reject
				// catalog shadowing rather than attribute a different source.
				if a.driver == "postgres" {
					for _, c := range a.catalog {
						if c.Schema == "pg_catalog" && c.Table == table {
							return ErrUnsupported
						}
					}
				}
				sch = a.schema
			}
			if SystemSchema(sch) {
				return ErrUnsupported
			}
			columns := []model.Output{}
			exact := false
			for _, c := range a.catalog {
				if c.Schema == sch && c.Table == table {
					exact = true
					break
				}
			}
			canonical := ""
			for _, c := range a.catalog {
				if (exact && c.Schema == sch && c.Table == table) || (!exact && a.equal(c.Schema, sch) && a.equal(c.Table, table)) {
					key := c.Schema + "\x00" + c.Table
					if canonical != "" && canonical != key {
						return ErrUnsupported
					}
					canonical = key
					columns = append(columns, model.Output{Name: c.Name, Type: c.Type, Sources: []model.Source{{Schema: c.Schema, Table: c.Table, Column: c.Name}}})
				}
			}
			if len(columns) == 0 {
				return fmt.Errorf("table is not in the configured schema catalog")
			}
			sch = columns[0].Sources[0].Schema
			table = columns[0].Sources[0].Table
			a.info.Tables = unique(append(a.info.Tables, model.Source{Schema: sch, Table: table}))
			s.relations = append(s.relations, relation{alias: al, schema: sch, table: table, columns: columns})
		case "JoinExpr", "JoinClause":
			if present(t, "IsNatural", "UsingClause", "Using", "Alias", "Hint") {
				return ErrUnsupported
			}
			if err := a.from(s, first(t, "Larg", "Left")); err != nil {
				return err
			}
			if err := a.from(s, first(t, "Rarg", "Right")); err != nil {
				return err
			}
			if _, err := a.expr(s, first(t, "Quals", "Condition"), false); err != nil {
				return err
			}
		case "RangeSubselect", "SubqueryRef", "SubqueryExpr":
			if present(t, "Lateral", "Restriction") {
				return ErrUnsupported
			}
			q := first(t, "Subquery", "Select", "Query")
			if operation(q) != "select" {
				return ErrUnsupported
			}
			o, err := a.statement(q, nil, s.ctes)
			if err != nil {
				return err
			}
			al := alias(t)
			if al == "" {
				return ErrUnsupported
			}
			if present(t, "Columns") {
				return ErrUnsupported
			}
			s.relations = append(s.relations, relation{alias: al, columns: o})
		case "AliasedTableRef":
			if present(t, "Columns", "WithTable", "TableSample", "Hints") {
				return ErrUnsupported
			}
			start := len(s.relations)
			if err := a.from(s, field(t, "Table")); err != nil {
				return err
			}
			if len(s.relations) != start+1 {
				return ErrUnsupported
			}
			s.relations[start].alias = alias(t)
		case "":
		default:
			return fmt.Errorf("unsupported table expression: %s", kind(t))
		}
	}
	return nil
}
func (a *analyzer) column(n any) (sch, table, col string, ok bool) {
	switch kind(n) {
	case "ColumnRef":
		if f := field(n, "Fields"); f != nil {
			parts := stringsIn(f)
			if len(parts) > 3 {
				return "", "", "", false
			}
			switch len(parts) {
			case 1:
				col = parts[0]
			case 2:
				table, col = parts[0], parts[1]
			case 3:
				sch, table, col = parts[0], parts[1], parts[2]
			}
			return sch, table, col, true
		}
		col = text(n, "Column")
		if present(n, "Star") {
			col = "*"
		}
		return text(n, "Schema"), text(n, "Table"), col, true
	case "StarExpr", "A_Star":
		return "", text(n, "Qualifier"), "*", true
	}
	return "", "", "", false
}
func (a *analyzer) resolve(s *scope, sch, table, col string, aliases bool) ([]model.Source, error) {
	if col == "" || col == "*" {
		return nil, ErrUnsupported
	}
	var found []model.Output
	for _, r := range s.relations {
		if a.driver == "sqlserver" && (r.alias == "inserted" || r.alias == "deleted") && table != r.alias {
			continue
		}
		if table != "" && !a.equal(table, r.alias) && !a.equal(table, r.table) {
			continue
		}
		if sch != "" && !a.equal(sch, r.schema) {
			continue
		}
		for _, o := range r.columns {
			if a.equal(o.Name, col) {
				found = append(found, o)
			}
		}
	}
	if len(found) == 0 && aliases && table == "" {
		for name, o := range s.aliases {
			if a.equal(name, col) {
				found = append(found, o)
			}
		}
	}
	if len(found) == 0 && s.outer != nil {
		return a.resolve(s.outer, sch, table, col, false)
	}
	if len(found) != 1 {
		return nil, fmt.Errorf("column source is ambiguous or unknown")
	}
	a.info.Reads = unique(append(a.info.Reads, found[0].Sources...))
	return found[0].Sources, nil
}
func (a *analyzer) star(s *scope, sch, table string) ([]model.Output, error) {
	out := []model.Output{}
	for _, r := range s.relations {
		if a.driver == "sqlserver" && (r.alias == "inserted" || r.alias == "deleted") && table != r.alias {
			continue
		}
		if table != "" && !a.equal(table, r.alias) && !a.equal(table, r.table) {
			continue
		}
		if sch != "" && !a.equal(sch, r.schema) {
			continue
		}
		out = append(out, r.columns...)
	}
	if len(out) == 0 {
		return nil, ErrUnsupported
	}
	for _, o := range out {
		a.info.Reads = unique(append(a.info.Reads, o.Sources...))
	}
	return out, nil
}
func unique(v []model.Source) []model.Source {
	out := []model.Source{}
	for _, x := range v {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// Functions are an explicit intersection of ordinary built-ins. Qualified calls,
// UDFs, sequences, file/network functions and procedures are rejected.
var functions = strings.Fields("count sum avg min max lower upper length char_length character_length len substring substr concat concat_ws trim ltrim rtrim replace abs round floor ceiling ceil coalesce nullif greatest least to_char to_date date_part date_trunc extract now current_timestamp current_date isnull ifnull json_extract json_value jsonb_extract_path_text st_astext st_asewkt st_srid st_x st_y")
var expressionKinds = strings.Fields("List ResTarget A_Const Integer Float Boolean String BitString Literal IntLit FloatLit StringLit BoolLit NullLit HexLit BitLit NullLiteral NumberLiteral StringLiteral BoolLiteral BooleanLiteral NullExpr ParamRef ParamMarkerExpr BindVariable VariableRef A_Expr BoolExpr BinaryExpr UnaryExpr UnaryOpExpr BinaryOpExpr ComparisonExpr CompareExpr BetweenExpr InExpr IsExpr IsNullExpr NullTest BooleanTest CaseExpr CaseWhen CaseWhenExpr WhenClause CoalesceExpr MinMaxExpr NullIfExpr ParenExpr SortBy OrderByItem Limit TopClause FetchClause FetchFirstClause WindowDef WindowFrame WindowFrameBound OverClause A_ArrayExpr ArrayExpr RowExpr SQLValueFunction OnCondition ValuesClause CollateExpr")

func (a *analyzer) expr(s *scope, n any, aliases bool) ([]model.Source, error) {
	if n == nil {
		return nil, nil
	}
	v := value(n)
	if !v.IsValid() {
		return nil, nil
	}
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		out := []model.Source{}
		for _, c := range items(n) {
			r, err := a.expr(s, c, aliases)
			if err != nil {
				return nil, err
			}
			out = unique(append(out, r...))
		}
		return out, nil
	}
	k := kind(n)
	if sch, t, c, ok := a.column(n); ok {
		if a.driver == "mysql" && sch != "" && sch != a.database {
			return nil, ErrUnsupported
		}
		if catalog := text(n, "Database"); catalog != "" && catalog != a.database {
			return nil, ErrUnsupported
		}
		if present(n, "Server") {
			return nil, ErrUnsupported
		}
		return a.resolve(s, sch, t, c, aliases)
	}
	if operation(n) != "" {
		if operation(n) != "select" {
			return nil, ErrUnsupported
		}
		o, err := a.statement(n, s, s.ctes)
		if err != nil {
			return nil, err
		}
		out := []model.Source{}
		for _, c := range o {
			out = unique(append(out, c.Sources...))
		}
		return out, nil
	}
	if k == "SubLink" || k == "SubqueryExpr" {
		q := first(n, "Subselect", "Select", "Query")
		o, err := a.expr(s, q, false)
		if err != nil {
			return nil, err
		}
		r, err := a.expr(s, first(n, "Testexpr"), false)
		return unique(append(o, r...)), err
	}
	if k == "FuncCall" || k == "FuncCallExpr" || k == "FunctionCall" {
		name := text(n, "Name")
		if a.driver == "postgres" {
			parts := stringsIn(field(n, "Funcname"))
			if len(parts) != 1 {
				return nil, ErrUnsupported
			}
			name = parts[0]
		} else if a.driver == "sqlserver" {
			x := field(n, "Name")
			if present(x, "Schema", "Database", "Server") {
				return nil, ErrUnsupported
			}
			name = text(x, "Object")
		}
		if present(n, "Schema") || !slices.Contains(functions, strings.ToLower(name)) {
			return nil, fmt.Errorf("function is outside the supported built-ins")
		}
		out := []model.Source{}
		for _, f := range []string{"Args", "AggOrder", "AggFilter", "Over", "OrderBy", "Within", "Separator"} {
			r, err := a.expr(s, field(n, f), aliases)
			if err != nil {
				return nil, err
			}
			out = unique(append(out, r...))
		}
		return out, nil
	}
	if k == "TypeCast" || k == "CastExpr" || k == "ConvertExpr" {
		// User-defined casts may execute user code. Only primitive data types.
		t := first(n, "TypeName", "DataType", "Type")
		names := stringsIn(first(t, "Names"))
		name := text(t, "Name")
		if len(names) > 0 {
			name = names[len(names)-1]
		}
		if !slices.Contains(strings.Fields("text varchar nvarchar char nchar integer int int2 int4 int8 bigint smallint numeric decimal float real float4 float8 double boolean bool date timestamp timestamptz time json jsonb"), strings.ToLower(name)) {
			return nil, ErrUnsupported
		}
		return a.expr(s, first(n, "Arg", "Expr", "Value"), aliases)
	}
	if !slices.Contains(expressionKinds, k) {
		return nil, fmt.Errorf("unsupported expression: %s", k)
	}
	if k == "VariableRef" && !strings.HasPrefix(strings.ToLower(text(n, "Name")), "@p") {
		return nil, ErrUnsupported
	}
	if k == "A_Expr" {
		ops := stringsIn(field(n, "Name"))
		if len(ops) != 1 || !slices.Contains(strings.Fields("= <> != < > <= >= + - * / % ^ || ~~ !~~ ~~* !~~* ~ !~ ~* !~*"), ops[0]) {
			return nil, ErrUnsupported
		}
	}
	out := []model.Source{}
	if v.Kind() != reflect.Struct {
		return nil, ErrUnsupported
	}
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		name := v.Type().Field(i).Name
		if name == "Loc" || !f.CanInterface() {
			continue
		}
		if f.Kind() != reflect.Pointer && f.Kind() != reflect.Interface && f.Kind() != reflect.Slice {
			continue
		}
		if f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.Uint8 {
			continue
		}
		r, err := a.expr(s, f.Interface(), aliases)
		if err != nil {
			return nil, err
		}
		out = unique(append(out, r...))
	}
	return out, nil
}
