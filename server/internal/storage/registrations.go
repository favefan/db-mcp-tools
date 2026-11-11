package storage

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"dbmcp.local/server/internal/model"
)

var ErrRegistrationPending = errors.New("entity registration is pending administrator authorization")

func (s *Store) IssueRegistration(name, expires string) (model.Registration, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return model.Registration{}, "", errors.New("registration name required, up to 200 bytes")
	}
	c, token, err := newCredential("", expires)
	if err != nil {
		return model.Registration{}, "", err
	}
	r := model.Registration{ID: c.ID, Name: name, Prefix: c.Prefix, Status: "issued", CreatedAt: time.Now().UTC().Format(time.RFC3339), ExpiresAt: c.ExpiresAt}
	_, err = s.DB.Exec("INSERT INTO registrations(id,digest,body) VALUES(?,?,?)", r.ID, Hash(token), encode(r))
	return r, token, err
}

func (s *Store) Registrations() ([]model.Registration, error) {
	return listJSON[model.Registration](s.DB, "SELECT body FROM registrations ORDER BY json_extract(body,'$.created_at') DESC,id")
}

func (s *Store) registrationAccess(entity string) error {
	var status string
	err := s.DB.QueryRow("SELECT json_extract(body,'$.status') FROM registrations WHERE entity_id=?", entity).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	switch status {
	case "approved":
		return nil
	case "pending":
		return ErrRegistrationPending
	default:
		return ErrDenied
	}
}

func (s *Store) claimRegistration(token, source string) (model.Identity, error) {
	if source == "" {
		return model.Identity{}, ErrDenied
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return model.Identity{}, err
	}
	defer tx.Rollback()
	rows, err := listJSON[model.Registration](tx, "SELECT body FROM registrations WHERE digest=?", Hash(token))
	if err != nil {
		return model.Identity{}, err
	}
	if len(rows) != 1 {
		return model.Identity{}, ErrDenied
	}
	r := rows[0]
	if r.ExpiresAt != "" && r.ExpiresAt <= time.Now().UTC().Format(time.RFC3339) {
		return model.Identity{}, ErrDenied
	}
	if r.Status == "pending" {
		return model.Identity{EntityID: r.EntityID, CredentialID: r.ID, Source: source}, ErrRegistrationPending
	}
	if r.Status != "issued" {
		return model.Identity{}, ErrDenied
	}
	e := model.Entity{ID: model.ID(), Name: r.Name, Enabled: false, Revision: 1, LastSeen: time.Now().UTC().Format(time.RFC3339), LastSource: source}
	if _, err = tx.Exec("INSERT INTO entities VALUES(?,?,?)", e.ID, e.Revision, encode(e)); err != nil {
		return model.Identity{}, err
	}
	c := model.Credential{ID: r.ID, EntityID: e.ID, Prefix: r.Prefix, ExpiresAt: r.ExpiresAt}
	if _, err = tx.Exec("INSERT INTO credentials VALUES(?,?,?,?)", c.ID, e.ID, Hash(token), encode(c)); err != nil {
		return model.Identity{}, err
	}
	r.EntityID, r.Status, r.Source, r.RequestedAt = e.ID, "pending", source, e.LastSeen
	if _, err = tx.Exec("UPDATE registrations SET entity_id=?,body=? WHERE id=?", e.ID, encode(r), r.ID); err != nil {
		return model.Identity{}, err
	}
	if _, err = tx.Exec("INSERT INTO audit_events(time,entity_id,instance_id,request_id,action,decision,detail,duration_ms) VALUES(?,?,?,?,?,?,?,?)", r.RequestedAt, e.ID, "", r.ID, "registration.request", "pending", "source="+source, 0); err != nil {
		return model.Identity{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.Identity{}, err
	}
	return model.Identity{EntityID: e.ID, CredentialID: c.ID, Source: source}, ErrRegistrationPending
}

// Approval creates all bindings and enables the entity in one transaction. A
// partial failure cannot make a pending credential usable.
func (s *Store) ApproveRegistration(id string, bindings []model.Binding) (model.Registration, error) {
	if len(bindings) == 0 || len(bindings) > 32 {
		return model.Registration{}, errors.New("select between 1 and 32 instances")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return model.Registration{}, err
	}
	defer tx.Rollback()
	rows, err := listJSON[model.Registration](tx, "SELECT body FROM registrations WHERE id=?", id)
	if err != nil {
		return model.Registration{}, err
	}
	if len(rows) != 1 {
		return model.Registration{}, sql.ErrNoRows
	}
	r := rows[0]
	if r.Status != "pending" {
		return r, ErrConflict
	}
	creds, err := listJSON[model.Credential](tx, "SELECT body FROM credentials WHERE id=?", id)
	if err != nil {
		return r, err
	}
	if len(creds) != 1 || creds[0].Revoked || (creds[0].ExpiresAt != "" && creds[0].ExpiresAt <= time.Now().UTC().Format(time.RFC3339)) {
		return r, ErrDenied
	}
	for _, b := range bindings {
		instances, err := listJSON[model.Instance](tx, "SELECT body FROM instances WHERE id=?", b.InstanceID)
		if err != nil {
			return r, err
		}
		if len(instances) != 1 || !instances[0].Enabled {
			return r, errors.New("selected instance is missing or disabled")
		}
		b.EntityID, b.Revision, b.ApprovalRules = r.EntityID, 1, nil
		if _, err = tx.Exec("INSERT INTO bindings VALUES(?,?,?,?)", b.EntityID, b.InstanceID, b.Revision, encode(b)); err != nil {
			return r, err
		}
	}
	entities, err := listJSON[model.Entity](tx, "SELECT body FROM entities WHERE id=?", r.EntityID)
	if err != nil {
		return r, err
	}
	if len(entities) != 1 {
		return r, ErrDenied
	}
	e := entities[0]
	e.Enabled = true
	e.Revision++
	if _, err = tx.Exec("UPDATE entities SET revision=?,body=? WHERE id=?", e.Revision, encode(e), e.ID); err != nil {
		return r, err
	}
	r.Status = "approved"
	if _, err = tx.Exec("UPDATE registrations SET body=? WHERE id=?", encode(r), r.ID); err != nil {
		return r, err
	}
	return r, tx.Commit()
}

func (s *Store) RevokeRegistration(id, decision string) error {
	if decision != "denied" && decision != "revoked" {
		return ErrDenied
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := listJSON[model.Registration](tx, "SELECT body FROM registrations WHERE id=?", id)
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return sql.ErrNoRows
	}
	r := rows[0]
	if decision == "denied" && r.Status != "pending" {
		return ErrConflict
	}
	if r.EntityID != "" {
		if _, err = tx.Exec("UPDATE entities SET revision=revision+1,body=json_set(body,'$.enabled',json('false'),'$.revision',revision+1) WHERE id=?", r.EntityID); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE credentials SET body=json_set(body,'$.revoked',json('true')) WHERE entity_id=?", r.EntityID); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE requests SET status='cancelled',payload=NULL WHERE entity_id=? AND status IN ('pending','approved')", r.EntityID); err != nil {
			return err
		}
	}
	r.Status = decision
	if _, err = tx.Exec("UPDATE registrations SET body=? WHERE id=?", encode(r), id); err != nil {
		return err
	}
	return tx.Commit()
}
