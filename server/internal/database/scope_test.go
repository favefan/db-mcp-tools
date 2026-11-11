package database

import (
	"context"
	"testing"
	"time"

	"dbmcp.local/server/internal/model"
)

func TestScopeSelectionNeverFallsThroughToAnotherDatabase(t *testing.T) {
	i := model.Instance{ID: "scope", Name: "scope", Driver: "postgres", Policy: model.DefaultPolicy(), Connection: model.Connection{Host: "127.0.0.1", Port: 5432, Database: "legacy", Username: "fixture", TLS: "disable"}}
	m := New()
	defer m.Close()
	ctx := context.Background()
	if got, err := m.Select(ctx, i, ""); err != nil || got.Connection.Database != "legacy" {
		t.Fatalf("legacy: %#v %v", got, err)
	}
	if _, err := m.Select(ctx, i, "other"); err == nil {
		t.Fatal("single scope escaped")
	}
	i.Connection.DatabaseMode = "selected"
	i.Connection.Databases = []string{"app", "Analytics"}
	for _, name := range []string{"", "legacy", "analytics", "app\x00"} {
		if _, err := m.Select(ctx, i, name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	selected, err := m.Select(ctx, i, "Analytics")
	if err != nil || selected.Connection.Database != "Analytics" {
		t.Fatal(err)
	}
	i.Connection.Databases = []string{"app"}
	if got, err := m.Select(ctx, i, ""); err != nil || got.Connection.Database != "app" {
		t.Fatal("single selected database not resolved")
	}
	for _, names := range [][]string{nil, {"app", "app"}, {""}} {
		i.Connection.Databases = names
		if Validate(i) == nil {
			t.Fatal("invalid selected scope accepted")
		}
	}
	i.Driver = "dm"
	i.Connection.Databases = []string{"one", "two"}
	if Validate(i) == nil {
		t.Fatal("multiple DM databases accepted for one endpoint")
	}
}

func TestPoolsSeparateTargetsAndShareInstanceConcurrency(t *testing.T) {
	m := New()
	defer m.Close()
	i := model.Instance{ID: "same-instance", Driver: "postgres", Policy: model.DefaultPolicy(), Connection: model.Connection{Host: "127.0.0.1", Port: 5432, Database: "first", Username: "fixture", TLS: "disable"}}
	first, err := m.get(i)
	if err != nil {
		t.Fatal(err)
	}
	i.Connection.Database = "second"
	second, err := m.get(i)
	if err != nil || first.db == second.db {
		t.Fatal("targets share a connection pool", err)
	}
	release, err := first.acquire(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = second.acquire(ctx, 1); err == nil {
		t.Fatal("switching database bypassed instance concurrency")
	}
	release()
	release, err = second.acquire(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	release()
	m.Invalidate(i.ID)
	if len(m.pools) != 0 {
		t.Fatal("instance edit retained database pools")
	}
}
