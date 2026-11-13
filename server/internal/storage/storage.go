// Package storage owns SQLite state, authenticated encryption and local secrets.
package storage

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dbmcp.local/server/internal/diagnostics"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/migrations"
	"github.com/gofrs/flock"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

var ErrConflict = errors.New("revision or request conflict")
var ErrDenied = errors.New("access denied")

const SchemaVersion = 6

type Store struct {
	DB         *sql.DB
	Dir        string
	AdminToken string
	ServerID   string
	aead       cipher.AEAD
	lock       *flock.Flock
}

func Open(dir string) (*Store, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	for _, name := range []string{"key", "admin.token", "state.sqlite"} {
		if info, e := os.Lstat(filepath.Join(dir, name)); e == nil && !info.Mode().IsRegular() {
			return nil, errors.New("state and secret files must be regular files, not links")
		} else if e != nil && !os.IsNotExist(e) {
			return nil, e
		}
	}
	_, existing := os.Stat(filepath.Join(dir, "state.sqlite"))
	if existing == nil {
		for _, name := range []string{"key", "admin.token"} {
			if _, err = os.Stat(filepath.Join(dir, name)); err != nil {
				return nil, errors.New("state is missing a matching key or administrator credential; restore a complete backup")
			}
		}
		err = Verify(dir)
	} else if os.IsNotExist(existing) {
		entries, e := os.ReadDir(dir)
		if e != nil {
			return nil, e
		}
		for _, entry := range entries {
			switch entry.Name() {
			case "deployment.toml", "key", "admin.token", "server.lock", "logs":
			default:
				return nil, errors.New("new data directory is not empty; choose a dedicated empty directory or restore a complete backup")
			}
		}
		err = Protect(dir)
	} else {
		return nil, existing
	}
	if err != nil {
		return nil, err
	}
	l := flock.New(filepath.Join(dir, "server.lock"))
	ok, err := l.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("data directory already in use")
	}
	s := &Store{Dir: dir, lock: l}
	success := false
	defer func() {
		if !success {
			s.Close()
		}
	}()
	key, err := secretFile(filepath.Join(dir, "key"), 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	s.aead, err = cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	token, err := secretFile(filepath.Join(dir, "admin.token"), 32)
	if err != nil {
		return nil, err
	}
	s.AdminToken = base64.RawURLEncoding.EncodeToString(token)
	s.DB, err = sql.Open("sqlite", filepath.Join(dir, "state.sqlite")+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s.DB.SetMaxOpenConns(1)
	goose.SetBaseFS(migrations.FS)
	if err = goose.SetDialect("sqlite3"); err != nil {
		return nil, err
	}
	goose.SetLogger(goose.NopLogger())
	v, err := goose.GetDBVersion(s.DB)
	if err != nil {
		return nil, err
	}
	if v > SchemaVersion {
		return nil, errors.New("state schema is newer than this server")
	}
	if v > 0 && v < SchemaVersion {
		if err = s.DB.QueryRow("SELECT value FROM settings WHERE key='server_id'").Scan(&s.ServerID); err != nil {
			return nil, err
		}
		if _, err = s.Backup(context.Background()); err != nil {
			return nil, fmt.Errorf("pre-migration backup: %w", err)
		}
	}
	if err = goose.Up(s.DB, "."); err != nil {
		return nil, err
	}
	s.ServerID = model.ID()
	_, err = s.DB.Exec("INSERT OR IGNORE INTO settings(key,value) VALUES('server_id',?)", s.ServerID)
	if err != nil {
		return nil, err
	}
	if err = s.DB.QueryRow("SELECT value FROM settings WHERE key='server_id'").Scan(&s.ServerID); err != nil {
		return nil, err
	}
	// A target transaction and local SQLite cannot share a commit. Never replay interrupted work.
	if _, err = s.DB.Exec("UPDATE requests SET status='result_unknown' WHERE status='executing'"); err != nil {
		return nil, err
	}
	success = true
	return s, nil
}
func secretFile(p string, n int) ([]byte, error) {
	b, err := os.ReadFile(p)
	if err == nil {
		if len(b) != n {
			return nil, fmt.Errorf("invalid secret file %s", filepath.Base(p))
		}
		return b, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	b = make([]byte, n)
	if _, err = rand.Read(b); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	return b, closeErr
}
func (s *Store) Close() error {
	if s.DB != nil {
		_ = s.DB.Close()
	}
	if s.lock != nil {
		return s.lock.Unlock()
	}
	return nil
}
func (s *Store) Seal(b []byte) []byte {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return s.aead.Seal(nonce, nonce, b, nil)
}
func (s *Store) Unseal(b []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(b) < n {
		return nil, errors.New("invalid encrypted payload")
	}
	return s.aead.Open(nil, b[:n], b[n:], nil)
}
func Hash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func encode(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
func checkRevision(r sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) SaveInstance(v model.Instance) (model.Instance, error) {
	secret := []byte{}
	if v.ID == "" {
		v.ID = model.ID()
	} else {
		old, err := s.Instance(v.ID, true)
		if err != nil {
			return v, err
		}
		if v.Connection.Password == "" {
			v.Connection.Password = old.Connection.Password
		}
	}
	secret = s.Seal([]byte(v.Connection.Password))
	v.PasswordSet = v.Connection.Password != ""
	v.Connection.Password = ""
	prev := v.Revision
	v.Revision++
	if prev == 0 {
		_, err := s.DB.Exec("INSERT INTO instances(id,revision,body,secret) VALUES(?,?,?,?)", v.ID, v.Revision, encode(v), secret)
		return v, err
	}
	err := checkRevision(s.DB.Exec("UPDATE instances SET revision=?,body=?,secret=? WHERE id=? AND revision=?", v.Revision, encode(v), secret, v.ID, prev))
	return v, err
}
func (s *Store) Instance(id string, secret bool) (model.Instance, error) {
	var v model.Instance
	var body string
	var b []byte
	err := s.DB.QueryRow("SELECT body,secret FROM instances WHERE id=?", id).Scan(&body, &b)
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal([]byte(body), &v); err != nil {
		return v, err
	}
	if secret {
		plain, err := s.Unseal(b)
		if err != nil {
			return v, err
		}
		v.Connection.Password = string(plain)
	}
	return v, nil
}
func (s *Store) Instances() ([]model.Instance, error) {
	return listJSON[model.Instance](s.DB, "SELECT body FROM instances ORDER BY id")
}

type queryer interface {
	Query(string, ...any) (*sql.Rows, error)
}

func listJSON[T any](db queryer, q string, args ...any) ([]T, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var b string
		var v T
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(b), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SaveEntity(v model.Entity) (model.Entity, error) {
	prev := v.Revision
	if v.ID == "" {
		v.ID = model.ID()
	}
	v.Revision++
	if prev == 0 {
		_, err := s.DB.Exec("INSERT INTO entities VALUES(?,?,?)", v.ID, v.Revision, encode(v))
		return v, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if err = checkRevision(tx.Exec("UPDATE entities SET revision=?,body=? WHERE id=? AND revision=?", v.Revision, encode(v), v.ID, prev)); err != nil {
		return v, err
	}
	if !v.Enabled {
		if _, err = tx.Exec("UPDATE requests SET status='cancelled',payload=NULL WHERE entity_id=? AND status IN ('pending','approved')", v.ID); err != nil {
			return v, err
		}
	}
	return v, tx.Commit()
}
func (s *Store) Entity(id string) (model.Entity, error) {
	v, err := listJSON[model.Entity](s.DB, "SELECT body FROM entities WHERE id=?", id)
	if err != nil {
		return model.Entity{}, err
	}
	if len(v) == 0 {
		return model.Entity{}, sql.ErrNoRows
	}
	return v[0], nil
}
func (s *Store) Entities() ([]model.Entity, error) {
	return listJSON[model.Entity](s.DB, "SELECT body FROM entities ORDER BY id")
}
func (s *Store) InstanceEntities(id string) ([]model.Entity, error) {
	return listJSON[model.Entity](s.DB, "SELECT entities.body FROM entities JOIN bindings ON bindings.entity_id=entities.id WHERE bindings.instance_id=? ORDER BY entities.id", id)
}
func (s *Store) SaveBinding(v model.Binding) (model.Binding, error) {
	if err := s.registrationAccess(v.EntityID); err != nil {
		return v, err
	}
	prev := v.Revision
	v.Revision++
	if prev == 0 {
		_, err := s.DB.Exec("INSERT INTO bindings VALUES(?,?,?,?)", v.EntityID, v.InstanceID, v.Revision, encode(v))
		return v, err
	}
	err := checkRevision(s.DB.Exec("UPDATE bindings SET revision=?,body=? WHERE entity_id=? AND instance_id=? AND revision=?", v.Revision, encode(v), v.EntityID, v.InstanceID, prev))
	return v, err
}
func (s *Store) Binding(entity, instance string) (model.Binding, error) {
	v, err := listJSON[model.Binding](s.DB, "SELECT body FROM bindings WHERE entity_id=? AND instance_id=?", entity, instance)
	if err != nil {
		return model.Binding{}, err
	}
	if len(v) == 0 {
		return model.Binding{}, ErrDenied
	}
	return v[0], nil
}
func (s *Store) Bindings(entity string) ([]model.Binding, error) {
	return listJSON[model.Binding](s.DB, "SELECT body FROM bindings WHERE entity_id=?", entity)
}
func (s *Store) SaveTemplate(v model.Template) (model.Template, error) {
	prev := v.Revision
	if v.ID == "" {
		v.ID = model.ID()
	}
	v.Revision++
	if prev == 0 {
		_, err := s.DB.Exec("INSERT INTO templates VALUES(?,?,?)", v.ID, v.Revision, encode(v))
		return v, err
	}
	err := checkRevision(s.DB.Exec("UPDATE templates SET revision=?,body=? WHERE id=? AND revision=?", v.Revision, encode(v), v.ID, prev))
	return v, err
}
func (s *Store) Templates() ([]model.Template, error) {
	return listJSON[model.Template](s.DB, "SELECT body FROM templates ORDER BY id")
}
func (s *Store) Issue(entity, expires string) (model.Credential, string, error) {
	if _, err := s.Entity(entity); err != nil {
		return model.Credential{}, "", err
	}
	v, token, err := newCredential(entity, expires)
	if err != nil {
		return v, "", err
	}
	_, err = s.DB.Exec("INSERT INTO credentials VALUES(?,?,?,?)", v.ID, entity, Hash(token), encode(v))
	return v, token, err
}
func newCredential(entity, expires string) (model.Credential, string, error) {
	if expires != "" {
		t, err := time.Parse(time.RFC3339, expires)
		if err != nil || !t.After(time.Now()) {
			return model.Credential{}, "", errors.New("expiry must be in the future")
		}
		expires = t.UTC().Format(time.RFC3339)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return model.Credential{}, "", err
	}
	token := "dbmcp_" + base64.RawURLEncoding.EncodeToString(b)
	v := model.Credential{ID: model.ID(), EntityID: entity, Prefix: token[:14], ExpiresAt: expires}
	return v, token, nil
}
func (s *Store) Credentials(entity string) ([]model.Credential, error) {
	return listJSON[model.Credential](s.DB, "SELECT body FROM credentials WHERE entity_id=?", entity)
}
func (s *Store) Revoke(id string) error {
	var registrationStatus string
	if err := s.DB.QueryRow("SELECT json_extract(body,'$.status') FROM registrations WHERE id=?", id).Scan(&registrationStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if registrationStatus == "issued" || registrationStatus == "pending" {
		return s.RevokeRegistration(id, "revoked")
	}
	v, err := listJSON[model.Credential](s.DB, "SELECT body FROM credentials WHERE id=?", id)
	if err != nil {
		return err
	}
	if len(v) != 1 {
		return sql.ErrNoRows
	}
	v[0].Revoked = true
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE credentials SET body=? WHERE id=?", encode(v[0]), id); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE requests SET status='cancelled',payload=NULL WHERE credential_id=? AND status IN ('pending','approved')", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Observe(entity, source, client string) error {
	if len(client) > 200 {
		client = client[:200]
	}
	_, err := s.DB.Exec("UPDATE entities SET body=json_set(body,'$.last_seen',?,'$.last_source',?,'$.client',?) WHERE id=?", time.Now().UTC().Format(time.RFC3339), source, client, entity)
	return err
}
func (s *Store) Rotate(id string) (model.Credential, string, error) {
	var entityID string
	if err := s.DB.QueryRow("SELECT entity_id FROM credentials WHERE id=?", id).Scan(&entityID); err != nil {
		return model.Credential{}, "", err
	}
	if err := s.registrationAccess(entityID); err != nil {
		return model.Credential{}, "", err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return model.Credential{}, "", err
	}
	defer tx.Rollback()
	old, err := listJSON[model.Credential](tx, "SELECT body FROM credentials WHERE id=?", id)
	if err != nil {
		return model.Credential{}, "", err
	}
	if len(old) != 1 || old[0].Revoked {
		return model.Credential{}, "", ErrDenied
	}
	c, token, err := newCredential(old[0].EntityID, old[0].ExpiresAt)
	if err != nil {
		return c, "", err
	}
	if _, err = tx.Exec("INSERT INTO credentials VALUES(?,?,?,?)", c.ID, c.EntityID, Hash(token), encode(c)); err != nil {
		return c, "", err
	}
	old[0].Revoked = true
	if _, err = tx.Exec("UPDATE credentials SET body=? WHERE id=?", encode(old[0]), id); err != nil {
		return c, "", err
	}
	if _, err = tx.Exec("UPDATE requests SET status='cancelled',payload=NULL WHERE credential_id=? AND status IN ('pending','approved')", id); err != nil {
		return c, "", err
	}
	if err = tx.Commit(); err != nil {
		return c, "", err
	}
	return c, token, nil
}
func (s *Store) Authenticate(token, source string) (model.Identity, error) {
	v, err := listJSON[model.Credential](s.DB, "SELECT body FROM credentials WHERE digest=?", Hash(token))
	if err != nil {
		return model.Identity{}, err
	}
	if len(v) != 1 {
		return s.claimRegistration(token, source)
	}
	c := v[0]
	if c.Revoked || (c.ExpiresAt != "" && c.ExpiresAt <= time.Now().UTC().Format(time.RFC3339)) {
		return model.Identity{}, ErrDenied
	}
	identity := model.Identity{EntityID: c.EntityID, CredentialID: c.ID, Source: source}
	if err = s.registrationAccess(c.EntityID); err != nil {
		return identity, err
	}
	e, err := s.Entity(c.EntityID)
	if err != nil || !e.Enabled {
		return model.Identity{}, ErrDenied
	}
	return model.Identity{EntityID: e.ID, CredentialID: c.ID, Source: source}, nil
}
func (s *Store) Recheck(i model.Identity) error {
	if err := s.registrationAccess(i.EntityID); err != nil {
		return err
	}
	v, err := listJSON[model.Credential](s.DB, "SELECT body FROM credentials WHERE id=? AND entity_id=?", i.CredentialID, i.EntityID)
	if err != nil {
		return err
	}
	if len(v) != 1 || v[0].Revoked || (v[0].ExpiresAt != "" && v[0].ExpiresAt <= time.Now().UTC().Format(time.RFC3339)) {
		return ErrDenied
	}
	e, err := s.Entity(i.EntityID)
	if err != nil || !e.Enabled {
		return ErrDenied
	}
	return nil
}

func (s *Store) CreateRequest(v model.Request, q model.Query) (model.Request, bool, error) {
	payload := s.Seal([]byte(encode(q)))
	_, err := s.DB.Exec("INSERT INTO requests(id,entity_id,credential_id,instance_id,client_request_id,digest,status,expires_at,body,payload) VALUES(?,?,?,?,?,?,?,?,?,?)", v.ID, v.EntityID, v.CredentialID, v.InstanceID, v.ClientRequestID, v.Digest, v.Status, v.ExpiresAt, requestBody(v), payload)
	if err == nil {
		return v, true, nil
	}
	var id string
	if e := s.DB.QueryRow("SELECT id FROM requests WHERE entity_id=? AND client_request_id=?", v.EntityID, v.ClientRequestID).Scan(&id); e != nil {
		return v, false, err
	}
	old, e := s.Request(id, false)
	if e != nil {
		return v, false, e
	}
	if old.Digest != v.Digest || old.CredentialID != v.CredentialID {
		return v, false, ErrConflict
	}
	return old, false, nil
}
func (s *Store) Request(id string, payload bool) (model.Request, error) {
	var v model.Request
	var body, status string
	var p, r []byte
	err := s.DB.QueryRow("SELECT body,status,payload,result FROM requests WHERE id=?", id).Scan(&body, &status, &p, &r)
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal([]byte(body), &v); err != nil {
		return v, err
	}
	v.Status = status
	if (status == "pending" || status == "approved") && v.ExpiresAt <= time.Now().UTC().Format(time.RFC3339) {
		res, err := s.DB.Exec("UPDATE requests SET status='expired',payload=NULL WHERE id=? AND status=?", id, status)
		if err != nil {
			return v, err
		}
		count, err := res.RowsAffected()
		if err != nil {
			return v, err
		}
		if count == 0 {
			return s.Request(id, payload)
		}
		v.Status = "expired"
		p = nil
	}
	if payload && len(p) > 0 {
		plain, err := s.Unseal(p)
		if err != nil {
			return v, err
		}
		v.Query = &model.Query{}
		if err = json.Unmarshal(plain, v.Query); err != nil {
			return v, err
		}
		v.ParameterJSON = encode(v.Query.Params)
	}
	if payload {
		if detail, detailErr := s.requestDetail(id); detailErr == nil {
			v.AgentReason = detail.AgentReason
		} else if !errors.Is(detailErr, sql.ErrNoRows) {
			return v, detailErr
		}
	}
	if len(r) > 0 {
		plain, err := s.Unseal(r)
		if err != nil {
			return v, err
		}
		v.Result = &model.Result{}
		if err = json.Unmarshal(plain, v.Result); err != nil {
			return v, err
		}
	}
	return v, nil
}
func (s *Store) Requests() ([]model.Request, error) {
	rows, err := s.DB.Query("SELECT id FROM requests ORDER BY CASE status WHEN 'pending' THEN 0 WHEN 'approved' THEN 1 WHEN 'executing' THEN 2 ELSE 3 END,rowid DESC LIMIT 200")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []model.Request{}
	for _, id := range ids {
		v, err := s.Request(id, false)
		if err != nil {
			return nil, err
		}
		v.Result = nil
		if detail, detailErr := s.requestDetail(id); detailErr == nil {
			v.AgentReason = detail.AgentReason
		} else if !errors.Is(detailErr, sql.ErrNoRows) {
			return nil, detailErr
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Store) Transition(id, from, to string) error {
	return checkRevision(s.DB.Exec("UPDATE requests SET status=? WHERE id=? AND status=? AND expires_at>?", to, id, from, time.Now().UTC().Format(time.RFC3339)))
}
func (s *Store) Claim(v model.Request) error {
	return checkRevision(s.DB.Exec("UPDATE requests SET status='executing',body=? WHERE id=? AND status='approved' AND expires_at>?", requestBody(v), v.ID, time.Now().UTC().Format(time.RFC3339)))
}
func requestBody(v model.Request) string {
	v.Query = nil
	v.AgentReason = ""
	v.Result = nil
	v.ParameterJSON = ""
	return encode(v)
}
func (s *Store) Finish(id string, r model.Result) error {
	r.RequestID = id
	return checkRevision(s.DB.Exec("UPDATE requests SET status=?,result=?,payload=NULL WHERE id=? AND status='executing'", r.Status, s.Seal([]byte(encode(r))), id))
}
func (s *Store) Audit(v model.Audit) error {
	_, err := s.DB.Exec("INSERT INTO audit_events(time,entity_id,instance_id,request_id,action,decision,detail,duration_ms) VALUES(?,?,?,?,?,?,?,?)", time.Now().UTC().Format(time.RFC3339), v.EntityID, v.InstanceID, v.RequestID, v.Action, v.Decision, v.Detail, v.DurationMS)
	if err != nil {
		slog.Error("audit.write_failed", "action", v.Action, "request_id", v.RequestID, "error", diagnostics.ErrorInfo(err))
	} else {
		slog.Info("audit.recorded", "action", v.Action, "entity_id", v.EntityID, "instance_id", v.InstanceID, "request_id", v.RequestID, "decision", v.Decision, "duration_ms", v.DurationMS)
	}
	return err
}
func (s *Store) Audits(entity, instance, decision string, before int64) ([]model.Audit, error) {
	return s.AuditsRange(entity, instance, decision, before, "", "")
}
func (s *Store) AuditsRange(entity, instance, decision string, before int64, from, to string) ([]model.Audit, error) {
	return s.auditsRange(entity, instance, decision, before, from, to, false)
}

// AuditRequestsRange shows one current summary per request; detail retains every event.
func (s *Store) AuditRequestsRange(entity, instance, decision string, before int64, from, to string) ([]model.Audit, error) {
	return s.auditsRange(entity, instance, decision, before, from, to, true)
}
func (s *Store) auditsRange(entity, instance, decision string, before int64, from, to string, grouped bool) ([]model.Audit, error) {
	if before == 0 {
		before = 1 << 62
	}
	// Keep immutable event decisions, but filter/display the request's current outcome.
	rows, err := s.DB.Query(`WITH events AS (
 SELECT a.*, COALESCE(CASE WHEN r.status IN ('pending','approved') AND r.expires_at<=? THEN 'expired' ELSE r.status END,
  (SELECT decision FROM audit_events terminal WHERE terminal.request_id=a.request_id AND a.request_id<>'' AND terminal.decision IN ('succeeded','failed','denied','expired','cancelled','result_unknown') ORDER BY terminal.id DESC LIMIT 1), a.decision) AS request_status
 FROM audit_events a LEFT JOIN requests r ON r.id=a.request_id
) SELECT id,time,entity_id,instance_id,request_id,action,decision,detail,
 CASE WHEN ? THEN COALESCE((SELECT duration_ms FROM audit_events execution WHERE execution.request_id=events.request_id AND events.request_id<>'' AND execution.action='execute' ORDER BY execution.id DESC LIMIT 1), duration_ms) ELSE duration_ms END,
 request_status FROM events
 WHERE (?=0 OR request_id='' OR id=(SELECT MAX(latest.id) FROM audit_events latest WHERE latest.request_id=events.request_id))
 AND id<? AND (?='' OR entity_id=? OR (?='__admin__' AND entity_id='')) AND (?='' OR instance_id=?)
 AND (?='' OR (CASE WHEN ? THEN request_status ELSE decision END)=?) AND (?='' OR time>=?) AND (?='' OR time<=?) ORDER BY id DESC LIMIT 200`,
		time.Now().UTC().Format(time.RFC3339), grouped, grouped, before, entity, entity, entity, instance, instance, decision, grouped, decision, from, from, to, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Audit{}
	for rows.Next() {
		var v model.Audit
		if err = rows.Scan(&v.ID, &v.Time, &v.EntityID, &v.InstanceID, &v.RequestID, &v.Action, &v.Decision, &v.Detail, &v.DurationMS, &v.RequestStatus); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Cleanup() error {
	_, err := s.DB.Exec("UPDATE requests SET status='expired',payload=NULL WHERE status IN ('pending','approved') AND expires_at<=?", time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("UPDATE requests SET result=NULL WHERE expires_at<=?", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("DELETE FROM audit_events WHERE time<?", time.Now().Add(-30*24*time.Hour).UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("DELETE FROM request_details WHERE created_at<?", time.Now().Add(-30*24*time.Hour).UTC().Format(time.RFC3339))
	return err
}
func (s *Store) Backup(ctx context.Context) (string, error) {
	dir := filepath.Join(s.Dir, "backups", time.Now().UTC().Format("20060102T150405.000000000"))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	// The snapshot inherits the already-private data directory's Windows ACL,
	// including the management user and service account. POSIX mkdir uses 0700.
	p := filepath.Join(dir, "state.sqlite")
	_, err := s.DB.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(p, "'", "''")+"'")
	if err != nil {
		return "", err
	}
	for _, name := range []string{"key", "admin.token"} {
		b, err := os.ReadFile(filepath.Join(s.Dir, name))
		if err != nil {
			return "", err
		}
		if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			return "", err
		}
	}
	if err = writeBackupManifest(dir, s.ServerID); err != nil {
		return "", err
	}
	return dir, nil
}
