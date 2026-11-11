-- +goose Up
-- Database-qualified object grants require a reader that understands their keys.
-- Preserve the implicit first selected database used by 1.0.1 for discovery.
UPDATE instances
SET
  body = json_set(
    body,
    '$.connection.database',
    json_extract(body, '$.connection.databases[0]')
  )
WHERE
  json_extract(body, '$.connection.database_mode') = 'selected'
  AND coalesce(json_extract(body, '$.connection.database'), '') = ''
  AND json_array_length(body, '$.connection.databases') > 0;

-- +goose Down
-- Restore a pre-upgrade backup to use an older application.
SELECT
  1;
