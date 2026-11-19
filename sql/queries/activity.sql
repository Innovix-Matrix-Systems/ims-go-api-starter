-- name: InsertActivity :exec
INSERT INTO activity_log (tenant_id, actor_id, action, subject_type, subject_id, changes, ip, request_id)
VALUES (sqlc.narg('tenant_id'), sqlc.narg('actor_id'), @action, @subject_type, sqlc.narg('subject_id'),
        @changes, sqlc.narg('ip'), sqlc.narg('request_id'));

-- name: ListActivity :many
-- Newest first, keyset-paginated on (created_at, id). all_tenants is only
-- set for platform accounts; otherwise rows are limited to one tenant.
SELECT a.id, a.tenant_id, a.actor_id, a.action, a.subject_type, a.subject_id, a.changes,
       a.ip, a.request_id, a.created_at, u.name AS actor_name
FROM activity_log a
LEFT JOIN users u ON u.id = a.actor_id
WHERE (@all_tenants::boolean OR a.tenant_id = @tenant_id)
  AND (sqlc.narg('subject_type')::text IS NULL OR a.subject_type = sqlc.narg('subject_type'))
  AND (sqlc.narg('subject_id')::bigint IS NULL OR a.subject_id = sqlc.narg('subject_id'))
  AND (sqlc.narg('actor_id')::bigint IS NULL OR a.actor_id = sqlc.narg('actor_id'))
  AND (sqlc.narg('before_at')::timestamptz IS NULL
       OR (a.created_at, a.id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::bigint))
ORDER BY a.created_at DESC, a.id DESC
LIMIT @row_limit;

-- name: PurgeActivity :execrows
DELETE FROM activity_log WHERE created_at < now() - make_interval(days => @days::int);
