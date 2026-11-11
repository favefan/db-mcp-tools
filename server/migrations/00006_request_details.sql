-- +goose Up
CREATE TABLE request_details (
  request_id TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  body BLOB NOT NULL
);

CREATE INDEX request_details_created ON request_details (created_at);

CREATE INDEX audit_request ON audit_events (request_id, id);

-- +goose Down
DROP INDEX audit_request;

DROP TABLE request_details;
