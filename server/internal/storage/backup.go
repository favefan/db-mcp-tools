package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"dbmcp.local/server/internal/model"
)

type BackupInfo struct {
	Path      string            `json:"path"`
	Version   string            `json:"version"`
	Schema    int               `json:"schema"`
	ServerID  string            `json:"server_id"`
	CreatedAt string            `json:"created_at"`
	Files     map[string]string `json:"files"`
}

func fileHash(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func writeBackupManifest(dir, id string) error {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "state.sqlite"))+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return err
	}
	var schema int
	err = db.QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied=1").Scan(&schema)
	_ = db.Close()
	if err != nil {
		return err
	}
	v := BackupInfo{Version: model.Version, Schema: schema, ServerID: id, CreatedAt: time.Now().UTC().Format(time.RFC3339), Files: map[string]string{}}
	for _, name := range []string{"state.sqlite", "key", "admin.token"} {
		hash, err := fileHash(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		v.Files[name] = hash
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600)
}
func ValidateBackup(dir string) (BackupInfo, error) {
	var v BackupInfo
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return v, errors.New("backup manifest is missing")
	}
	if err = json.Unmarshal(b, &v); err != nil || v.Schema < 1 || v.Schema > SchemaVersion {
		return v, errors.New("unsupported backup schema")
	}
	for _, name := range []string{"state.sqlite", "key", "admin.token"} {
		hash, err := fileHash(filepath.Join(dir, name))
		if err != nil || hash != v.Files[name] {
			return v, errors.New("backup file hash mismatch")
		}
	}
	for _, name := range []string{"key", "admin.token"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(b) != 32 {
			return v, errors.New("backup secret has invalid length")
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "state.sqlite"))+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return v, err
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return v, errors.New("backup SQLite integrity check failed")
	}
	var schema int
	if err = db.QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied=1").Scan(&schema); err != nil || schema != v.Schema {
		return v, errors.New("backup database version does not match manifest")
	}
	v.Path = dir
	return v, nil
}
func (s *Store) Backups() ([]BackupInfo, error) {
	root := filepath.Join(s.Dir, "backups")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []BackupInfo{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		b, err := os.ReadFile(filepath.Join(p, "manifest.json"))
		if err != nil {
			continue
		}
		var v BackupInfo
		if json.Unmarshal(b, &v) != nil {
			continue
		}
		v.Path = p
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b BackupInfo) int {
		if a.CreatedAt < b.CreatedAt {
			return 1
		}
		if a.CreatedAt > b.CreatedAt {
			return -1
		}
		if a.Path < b.Path {
			return 1
		}
		if a.Path > b.Path {
			return -1
		}
		return 0
	})
	return out, nil
}
func (s *Store) RetainBackups(keep int) error {
	backups, err := s.Backups()
	if err != nil {
		return err
	}
	if keep < 1 {
		return errors.New("backup retention must be positive")
	}
	for _, v := range backups[min(keep, len(backups)):] {
		entries, err := os.ReadDir(v.Path)
		if err != nil {
			return err
		}
		safe := true
		for _, e := range entries {
			if e.IsDir() || !slices.Contains([]string{"state.sqlite", "key", "admin.token", "manifest.json"}, e.Name()) {
				safe = false
			}
		}
		if !safe {
			continue
		}
		for _, e := range entries {
			if err = os.Remove(filepath.Join(v.Path, e.Name())); err != nil {
				return err
			}
		}
		if err = os.Remove(v.Path); err != nil {
			return err
		}
	}
	return nil
}
