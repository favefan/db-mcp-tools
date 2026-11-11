-- +goose Up
CREATE TABLE settings (KEY TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE instances (
  id TEXT PRIMARY KEY,
  revision INTEGER NOT NULL,
  body TEXT NOT NULL,
  secret BLOB NOT NULL
);

CREATE TABLE templates (
  id TEXT PRIMARY KEY,
  revision INTEGER NOT NULL,
  body TEXT NOT NULL
);

CREATE TABLE entities (
  id TEXT PRIMARY KEY,
  revision INTEGER NOT NULL,
  body TEXT NOT NULL
);

CREATE TABLE credentials (
  id TEXT PRIMARY KEY,
  entity_id TEXT NOT NULL REFERENCES entities (id),
  digest TEXT NOT NULL UNIQUE,
  body TEXT NOT NULL
);

CREATE TABLE bindings (
  entity_id TEXT NOT NULL REFERENCES entities (id),
  instance_id TEXT NOT NULL REFERENCES instances (id),
  revision INTEGER NOT NULL,
  body TEXT NOT NULL,
  PRIMARY KEY (entity_id, instance_id)
);

CREATE TABLE requests (
  id TEXT PRIMARY KEY,
  entity_id TEXT NOT NULL REFERENCES entities (id),
  credential_id TEXT NOT NULL REFERENCES credentials (id),
  instance_id TEXT NOT NULL REFERENCES instances (id),
  client_request_id TEXT NOT NULL,
  digest TEXT NOT NULL,
  status TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  body TEXT NOT NULL,
  payload BLOB,
  result BLOB,
  UNIQUE (entity_id, client_request_id)
);

CREATE INDEX requests_status ON requests (status, expires_at);

CREATE TABLE audit_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  time TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  ACTION TEXT NOT NULL,
  decision TEXT NOT NULL,
  detail TEXT NOT NULL,
  duration_ms INTEGER NOT NULL
);

CREATE INDEX audit_time ON audit_events (time);
