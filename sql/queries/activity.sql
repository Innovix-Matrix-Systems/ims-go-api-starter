-- name: InsertActivity :exec
INSERT INTO activity_log (tenant_id, actor_id, impersonator_id, impersonator_name, action, subject_type,
                          subject_id, changes, ip, request_id)
VALUES (sqlc.narg('tenant_id'), sqlc.narg('actor_id'), sqlc.narg('impersonator_id'), sqlc.narg('impersonator_name'),
        @action, @subject_type, sqlc.narg('subject_id'), @changes, sqlc.narg('ip'), sqlc.narg('request_id'));

-- name: ListActivity :many
-- Newest first, keyset-paginated on (created_at, id). The service picks the
-- tenant predicate: system_only = the system audit log (tenant_id IS NULL);
-- otherwise all_tenants, or exactly tenant_id. all_tenants and system_only
-- are only set for activity.view.all readers.
SELECT a.id, a.tenant_id, a.actor_id, a.impersonator_id, a.impersonator_name, a.action, a.subject_type,
       a.subject_id, a.changes, a.ip, a.request_id, a.created_at, u.name AS actor_name
FROM activity_log a
LEFT JOIN users u ON u.id = a.actor_id
WHERE CASE
        WHEN @system_only::boolean THEN a.tenant_id IS NULL
        ELSE (@all_tenants::boolean OR a.tenant_id = sqlc.narg('tenant_id')::bigint)
      END
  AND (sqlc.narg('subject_type')::text IS NULL OR a.subject_type = sqlc.narg('subject_type'))
  AND (sqlc.narg('subject_id')::bigint IS NULL OR a.subject_id = sqlc.narg('subject_id'))
  AND (sqlc.narg('actor_id')::bigint IS NULL OR a.actor_id = sqlc.narg('actor_id'))
  AND (sqlc.narg('impersonator_id')::bigint IS NULL OR a.impersonator_id = sqlc.narg('impersonator_id'))
  AND (NOT @impersonated_only::boolean OR a.impersonator_id IS NOT NULL OR a.action LIKE 'impersonation.%')
  AND (sqlc.narg('before_at')::timestamptz IS NULL
       OR (a.created_at, a.id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::bigint))
ORDER BY a.created_at DESC, a.id DESC
LIMIT @row_limit;

-- name: PurgeActivity :execrows
DELETE FROM activity_log WHERE created_at < now() - make_interval(days => @days::int);
