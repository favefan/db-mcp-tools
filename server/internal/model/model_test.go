package model

import "testing"

func TestResourceNamesCannotChangeGrantStructure(t *testing.T) {
	p := Policy{Resources: []string{Resource("a", "b", "c")}}
	if !p.AllowsColumn("app", "a", "b", "c") || p.AllowsColumn("app", "a.b", "c", "secret") {
		t.Fatal("literal separators changed the grant structure")
	}
	p.Resources = []string{Resource("public", "users*")}
	if !p.AllowsColumn("app", "public", "users*", "id") || p.AllowsColumn("app", "public", "users_private", "id") {
		t.Fatal("literal glob became a permission pattern")
	}
	p.Resources = []string{Resource("my.schema") + ".*"}
	if !p.AllowsColumn("app", "my.schema", "t/a", "a?b") || p.AllowsColumn("app", "my", "schema", "secret") {
		t.Fatal("schema pattern escaped its component")
	}
	p.Resources = []string{Resource("public", "users%2A")}
	if p.AllowsTable("app", "public", "users*") {
		t.Fatal("encoded-looking literal collided with encoded wildcard")
	}
}

func TestDatabaseQualifiedScopesCannotAuthorizeSameNamesElsewhere(t *testing.T) {
	p := Policy{Resources: []string{DatabaseResource("one", "public", "users", "email")}}
	if !p.AllowsDatabase("one") || p.AllowsDatabase("two") || !p.AllowsTable("one", "public", "users") || p.AllowsTable("two", "public", "users") {
		t.Fatal("database scope escaped")
	}
	if !p.AllowsColumn("one", "public", "users", "email") || p.AllowsColumn("one", "public", "users", "password") || p.AllowsColumn("two", "public", "users", "email") {
		t.Fatal("column scope escaped")
	}
	for _, database := range []string{"a/b", "a.b", "a*", "a%2F", "数据库"} {
		p.Resources = []string{DatabaseResource(database) + "/*"}
		if !p.AllowsDatabase(database) || !p.AllowsColumn(database, "public", "users", "id") || p.AllowsDatabase(database+"other") {
			t.Fatalf("database encoding collision %q", database)
		}
	}
	p.Resources = []string{DatabaseResource("one", "public") + ".*"}
	if !p.AllowsColumn("one", "public", "new_table", "new_column") || p.AllowsColumn("two", "public", "new_table", "new_column") {
		t.Fatal("schema scope escaped")
	}
	p.Resources = []string{"public.users"}
	if !p.AllowsColumn("one", "public", "users", "id") || !p.AllowsColumn("two", "public", "users", "id") {
		t.Fatal("legacy relative scope changed")
	}
}
