package storage

import (
	"context"
	"dbmcp.local/server/internal/model"
)

func (s *Store) Export(ctx context.Context) (model.Configuration, error) {
	c := model.Configuration{Version: model.APIVersion}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	if c.Instances, err = listJSON[model.Instance](tx, "SELECT body FROM instances ORDER BY id"); err != nil {
		return c, err
	}
	for n := range c.Instances {
		c.Instances[n].PasswordSet = false
		c.Instances[n].Connection.Password = ""
	}
	if c.Entities, err = listJSON[model.Entity](tx, "SELECT body FROM entities ORDER BY id"); err != nil {
		return c, err
	}
	for n := range c.Entities {
		c.Entities[n].LastSeen = ""
		c.Entities[n].LastSource = ""
	}
	if c.Bindings, err = listJSON[model.Binding](tx, "SELECT body FROM bindings ORDER BY entity_id,instance_id"); err != nil {
		return c, err
	}
	if c.Templates, err = listJSON[model.Template](tx, "SELECT body FROM templates ORDER BY id"); err != nil {
		return c, err
	}
	return c, tx.Commit()
}

// Import inserts a validated configuration as new, disabled objects. Existing
// connections, credentials and pending requests remain untouched.
func (s *Store) Import(ctx context.Context, c model.Configuration) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	instances, entities := map[string]string{}, map[string]string{}
	for _, v := range c.Instances {
		old := v.ID
		v.ID = model.ID()
		instances[old] = v.ID
		v.Enabled = false
		v.PasswordSet = false
		v.Connection.Password = ""
		v.Revision = 1
		if _, err = tx.Exec("INSERT INTO instances VALUES(?,?,?,?)", v.ID, v.Revision, encode(v), s.Seal(nil)); err != nil {
			return err
		}
	}
	for _, v := range c.Entities {
		old := v.ID
		v.ID = model.ID()
		entities[old] = v.ID
		v.Enabled = false
		v.LastSeen = ""
		v.LastSource = ""
		v.Revision = 1
		if _, err = tx.Exec("INSERT INTO entities VALUES(?,?,?)", v.ID, v.Revision, encode(v)); err != nil {
			return err
		}
	}
	for _, v := range c.Bindings {
		v.EntityID = entities[v.EntityID]
		v.InstanceID = instances[v.InstanceID]
		v.Revision = 1
		if _, err = tx.Exec("INSERT INTO bindings VALUES(?,?,?,?)", v.EntityID, v.InstanceID, v.Revision, encode(v)); err != nil {
			return err
		}
	}
	for _, v := range c.Templates {
		v.ID = model.ID()
		v.Revision = 1
		if _, err = tx.Exec("INSERT INTO templates VALUES(?,?,?)", v.ID, v.Revision, encode(v)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) RememberAndApprove(ctx context.Context, requestID string, b model.Binding) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	prev := b.Revision
	b.Revision++
	if err = checkRevision(tx.Exec("UPDATE bindings SET revision=?,body=? WHERE entity_id=? AND instance_id=? AND revision=?", b.Revision, encode(b), b.EntityID, b.InstanceID, prev)); err != nil {
		return err
	}
	if err = checkRevision(tx.Exec("UPDATE requests SET status='approved' WHERE id=? AND status='pending' AND expires_at>strftime('%Y-%m-%dT%H:%M:%SZ','now')", requestID)); err != nil {
		return err
	}
	return tx.Commit()
}
