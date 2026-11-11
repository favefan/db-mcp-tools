-- +goose Up
CREATE TABLE registrations (
  id TEXT PRIMARY KEY,
  digest TEXT NOT NULL UNIQUE,
  entity_id TEXT UNIQUE REFERENCES entities (id),
  body TEXT NOT NULL
);

-- +goose Down
DROP TABLE registrations;
