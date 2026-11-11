-- +goose Up
-- Pin legacy requests and remembered approvals to their original single database.
UPDATE requests
SET
  body = json_set(
    body,
    '$.database',
    (
      SELECT
        json_extract(instances.body, '$.connection.database')
      FROM
        instances
      WHERE
        instances.id = requests.instance_id
    )
  );

-- A changed legacy instance no longer proves the original target database.
UPDATE requests
SET
  status = 'cancelled',
  payload = NULL
WHERE
  status IN ('pending', 'approved')
  AND json_extract(body, '$.instance_revision') != (
    SELECT
      revision
    FROM
      instances
    WHERE
      instances.id = requests.instance_id
  );

UPDATE bindings
SET
  body = json_set(
    body,
    '$.approval_rules',
    json(
      (
        SELECT
          json_group_array(
            json_set(
              value,
              '$.database',
              (
                SELECT
                  json_extract(instances.body, '$.connection.database')
                FROM
                  instances
                WHERE
                  instances.id = bindings.instance_id
              )
            )
          )
        FROM
          json_each(bindings.body, '$.approval_rules')
      )
    )
  )
WHERE
  json_array_length(body, '$.approval_rules') > 0;

-- +goose Down
-- Application state is forward-only; restoring a pre-upgrade backup is required.
SELECT
  1;
