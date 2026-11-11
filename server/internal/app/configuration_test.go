package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
)

func TestConfigurationExchangeDoesNotExportOrActivateSecrets(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	p := model.DefaultPolicy()
	i, err := s.SaveInstance(model.Instance{Name: "existing", Driver: "postgres", Enabled: true, Policy: p, Connection: model.Connection{Host: "127.0.0.1", Port: 5432, Database: "fixture", Username: "fixture", Password: "database-secret", TLS: "require"}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.SaveEntity(model.Entity{Name: "existing-agent", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := s.Issue(e.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveBinding(model.Binding{EntityID: e.ID, InstanceID: i.ID, Policy: p}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(c)
	if bytes.Contains(encoded, []byte("database-secret")) || bytes.Contains(encoded, []byte(token)) {
		t.Fatal("configuration leaked secrets")
	}
	plan, err := a.ImportPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Import(context.Background(), c, "changed-digest"); err == nil {
		t.Fatal("changed import preview was accepted")
	}
	if err = a.Import(context.Background(), c, plan["digest"].(string)); err != nil {
		t.Fatal(err)
	}
	all, err := s.Instances()
	if err != nil || len(all) != 2 {
		t.Fatal("import overwrote existing state")
	}
	for _, v := range all {
		if v.ID == i.ID {
			if !v.Enabled {
				t.Fatal("original instance was disabled")
			}
			continue
		}
		if v.Enabled || v.PasswordSet {
			t.Fatal("imported connection was activated")
		}
	}
	if _, err = s.Authenticate(token, "127.0.0.1"); err != nil {
		t.Fatal("import revoked an unrelated credential")
	}
	broken := model.Configuration{Version: 1, Instances: c.Instances, Entities: c.Entities, Bindings: []model.Binding{{EntityID: "missing", InstanceID: i.ID, Policy: p}}}
	if err = s.Import(context.Background(), broken); err == nil {
		t.Fatal("orphan binding accepted")
	}
	all, err = s.Instances()
	if err != nil || len(all) != 2 {
		t.Fatal("failed import left partial objects")
	}
}
