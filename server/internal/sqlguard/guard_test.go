package sqlguard

import (
	"slices"
	"testing"

	"dbmcp.local/server/internal/model"
)

func catalog(schema string) []model.Column {
	out := []model.Column{}
	for _, table := range []string{"users", "orders"} {
		for i, name := range []string{"id", "email", "password", "user_id", "name"} {
			out = append(out, model.Column{Schema: schema, Table: table, Name: name, Ordinal: i + 1})
		}
	}
	return out
}
func TestSupportedSources(t *testing.T) {
	cases := []struct {
		driver, schema, sql string
		outputs             int
		source              string
	}{
		{"postgres", "public", `SELECT u.email AS e FROM public.users u WHERE u.password = $1 ORDER BY e`, 1, "password"},
		{"postgres", "public", `SELECT u.*, o.id AS order_id FROM public.users u JOIN public.orders o ON o.user_id=u.id`, 6, "user_id"},
		{"postgres", "public", `WITH x AS (SELECT email AS e FROM public.users) SELECT e FROM x`, 1, "email"},
		{"postgres", "public", `SELECT x.e FROM (SELECT upper(email) AS e FROM public.users) x`, 1, "email"},
		{"postgres", "public", `SELECT email FROM public.users WHERE id IN (SELECT user_id FROM public.orders)`, 1, "user_id"},
		{"postgres", "public", `UPDATE public.users SET name=$1 WHERE id=$2 RETURNING email AS e`, 1, "email"},
		{"postgres", "public", `INSERT INTO public.users(id,email) VALUES($1,$2) RETURNING id`, 1, "email"},
		{"mysql", "app", "SELECT u.email AS e FROM users u WHERE u.password = ? ORDER BY e", 1, "password"},
		{"mysql", "app", "WITH x AS (SELECT email AS e FROM users) SELECT e FROM x", 1, "email"},
		{"mysql", "app", "UPDATE users SET name=? WHERE id=?", 0, "name"},
		{"mysql", "app", "UPDATE users SET id=id+1,name='changed' WHERE id=2", 0, "id"},
		{"mysql", "app", "INSERT INTO users(id,email) VALUES(?,?)", 0, "email"},
		{"sqlserver", "dbo", `SELECT TOP (10) u.[email] AS [e] FROM [dbo].[users] u WHERE u.password=@p1 ORDER BY e`, 1, "password"},
		{"sqlserver", "dbo", `WITH x AS (SELECT email AS e FROM dbo.users) SELECT e FROM x`, 1, "email"},
		{"sqlserver", "dbo", `UPDATE dbo.users SET name=@p1 OUTPUT inserted.email WHERE id=@p2`, 1, "name"},
		{"dm", "APP", `SELECT u.email AS e FROM APP.users u WHERE u.password=:1`, 1, "password"},
		{"dm", "APP", `UPDATE APP.users SET name=:1 WHERE id=:2`, 0, "name"},
	}
	for _, c := range cases {
		t.Run(c.driver+"/"+c.sql, func(t *testing.T) {
			v, err := Analyze(c.driver, "app", c.schema, c.sql, catalog(c.schema))
			if err != nil {
				t.Fatal(err)
			}
			if len(v.Outputs) != c.outputs {
				t.Fatalf("outputs=%#v", v.Outputs)
			}
			if !slices.ContainsFunc(v.Reads, func(s model.Source) bool { return s.Column == c.source }) {
				t.Fatalf("missing %s in %#v", c.source, v.Reads)
			}
			if len(v.Tables) == 0 {
				t.Fatal("no table sources")
			}
		})
	}
}
func TestRejectUnsafeAndUnresolvedSQL(t *testing.T) {
	cases := []struct{ driver, sql string }{
		{"postgres", `SELECT email FROM public.users; DELETE FROM public.users`},
		{"postgres", `WITH x AS (DELETE FROM public.users RETURNING email) SELECT * FROM x`},
		{"postgres", `SELECT pg_read_file('/etc/passwd')`},
		{"postgres", `SELECT public.lower(email) FROM public.users`},
		{"postgres", `SELECT email INTO stolen FROM public.users`},
		{"postgres", `SELECT email FROM public.users FOR UPDATE`},
		{"postgres", `SELECT u.email FROM public.users u JOIN public.orders o USING(id)`},
		{"postgres", `SELECT email FROM public.users u JOIN public.orders o ON o.id=u.id`},
		{"postgres", `SELECT x.email FROM public.users x(id)`},
		{"postgres", `SELECT not_a_column FROM public.users`},
		{"mysql", `SELECT email INTO OUTFILE '/tmp/stolen' FROM users`},
		{"mysql", `SELECT load_file('/etc/passwd')`},
		{"mysql", `SELECT @x := email FROM users`},
		{"sqlserver", `SELECT email INTO dbo.stolen FROM dbo.users`},
		{"sqlserver", `EXEC('SELECT email FROM dbo.users')`},
		{"sqlserver", `SELECT * FROM OPENROWSET('x','y','z')`},
		{"dm", `SELECT email FROM APP.users@remote`},
		{"dm", `BEGIN DELETE FROM APP.users; END;`},
	}
	for _, c := range cases {
		t.Run(c.driver+"/"+c.sql, func(t *testing.T) {
			schema := map[string]string{"postgres": "public", "mysql": "app", "sqlserver": "dbo", "dm": "APP"}[c.driver]
			if _, err := Analyze(c.driver, "app", schema, c.sql, catalog(schema)); err == nil {
				t.Fatal("unsafe SQL accepted")
			}
		})
	}
}

func TestDerivedLineageSurvivesScopes(t *testing.T) {
	for _, q := range []string{`SELECT x.e FROM (SELECT upper(email) AS e FROM public.users) x`, `WITH x AS (SELECT upper(email) AS e FROM public.users) SELECT e FROM x`} {
		v, err := Analyze("postgres", "app", "public", q, catalog("public"))
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Outputs) != 1 || !v.Outputs[0].Derived {
			t.Fatalf("derived mask lineage lost: %#v", v.Outputs)
		}
	}
}

func TestCaseSensitiveResourcesDoNotMerge(t *testing.T) {
	for _, driver := range []string{"mysql", "sqlserver", "dm"} {
		columns := []model.Column{{Schema: "app", Table: "Users", Name: "id"}, {Schema: "app", Table: "users", Name: "id"}}
		query := "SELECT id FROM app.users"
		if driver == "dm" {
			query = `SELECT "id" FROM "app"."users"`
		}
		info, err := Analyze(driver, "app", "app", query, columns)
		if err != nil {
			t.Fatalf("%s: %v", driver, err)
		}
		if len(info.Tables) != 1 || info.Tables[0].Table != "users" {
			t.Fatalf("%s merged case-sensitive table identities: %#v", driver, info.Tables)
		}
	}
	if _, err := Analyze("sqlserver", "app", "dbo", "SELECT id FROM APP.dbo.users", catalog("dbo")); err == nil {
		t.Fatal("case-folded cross-catalog reference was accepted")
	}
	for _, query := range []string{"SELECT id FROM APP.users", "SELECT APP.users.id FROM app.users", "UPDATE APP.users SET id=1", "DELETE FROM APP.users", "INSERT INTO APP.users(id) VALUES(1)"} {
		if _, err := Analyze("mysql", "app", "app", query, catalog("app")); err == nil {
			t.Fatalf("cross-database MySQL reference accepted: %s", query)
		}
	}
}
