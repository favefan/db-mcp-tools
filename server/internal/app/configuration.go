package app

import (
	"context"
	"encoding/json"
	"errors"

	"dbmcp.local/server/internal/database"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
)

func (a *App) ImportPlan(c model.Configuration) (map[string]any, error) {
	if c.Version != model.APIVersion {
		return nil, errors.New("unsupported configuration version")
	}
	instances, entities, templates := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, v := range c.Instances {
		if v.ID == "" || instances[v.ID] {
			return nil, errors.New("duplicate or missing instance ID")
		}
		instances[v.ID] = true
		if v.Connection.Password != "" {
			return nil, errors.New("configuration imports must not contain secrets")
		}
		if err := database.Validate(v); err != nil {
			return nil, err
		}
		if err := ValidatePolicy(v.Policy); err != nil {
			return nil, err
		}
	}
	for _, v := range c.Entities {
		if v.ID == "" || entities[v.ID] || v.Name == "" {
			return nil, errors.New("duplicate or invalid entity")
		}
		entities[v.ID] = true
	}
	seen := map[string]bool{}
	for _, b := range c.Bindings {
		key := b.EntityID + "/" + b.InstanceID
		if !entities[b.EntityID] || !instances[b.InstanceID] || seen[key] {
			return nil, errors.New("binding references missing objects or is duplicated")
		}
		seen[key] = true
		if err := ValidatePolicy(b.Policy); err != nil {
			return nil, err
		}
		if err := ValidateApprovalRules(b.ApprovalRules); err != nil {
			return nil, err
		}
	}
	for _, v := range c.Templates {
		if v.ID == "" || v.Name == "" || templates[v.ID] {
			return nil, errors.New("invalid template")
		}
		templates[v.ID] = true
		if err := ValidatePolicy(v.Policy); err != nil {
			return nil, err
		}
	}
	encoded, _ := json.Marshal(c)
	return map[string]any{"digest": digestConfiguration(encoded), "instances": len(c.Instances), "entities": len(c.Entities), "bindings": len(c.Bindings), "templates": len(c.Templates), "behavior": "create new disabled objects; preserve existing state; re-enter passwords and issue new credentials"}, nil
}
func digestConfiguration(b []byte) string { return storage.Hash(string(b)) }
func (a *App) Import(ctx context.Context, c model.Configuration, expected string) error {
	plan, err := a.ImportPlan(c)
	if err != nil {
		return err
	}
	if expected == "" || plan["digest"] != expected {
		return errors.New("configuration changed; preview the import again")
	}
	if err = a.Store.Import(ctx, c); err != nil {
		return err
	}
	a.Changed()
	return a.Store.Audit(model.Audit{Action: "configuration.import", Decision: "saved", Detail: "digest=" + expected})
}
