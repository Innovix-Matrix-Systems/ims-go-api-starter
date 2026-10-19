-- name: InsertDevAction :one
INSERT INTO dev_actions (actor_user_id, action, target, params, status, error_message, duration_ms)
VALUES (@actor_user_id, @action, @target, @params, @status, @error_message, @duration_ms)
RETURNING *;

-- name: ListDevActions :many
SELECT * FROM dev_actions
WHERE (@before_id::bigint = 0 OR id < @before_id::bigint)
ORDER BY id DESC
LIMIT @row_limit;
