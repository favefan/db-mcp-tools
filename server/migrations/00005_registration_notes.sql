-- +goose Up
UPDATE entities
SET
  revision = revision + 1,
  body = json_set(body, '$.notes', '', '$.revision', revision + 1)
WHERE
  json_extract(body, '$.notes') = 'Agent 首次接入登记，等待管理员授权'
  AND id IN (
    SELECT
      entity_id
    FROM
      registrations
    WHERE
      entity_id IS NOT NULL
  );

-- +goose Down
-- Status belongs to registrations; do not restore obsolete status text in notes.
SELECT
  1;
