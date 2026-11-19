-- name: InsertDevAction :one
INSERT INTO dev_actions (actor_user_id, action, target, params, status, error_message, duration_ms)
VALUES (@actor_user_id, @action, @target, @params, @status, @error_message, @duration_ms)
RETURNING *;

-- name: ListDevActions :many
SELECT * FROM dev_actions
WHERE (@before_id::bigint = 0 OR id < @before_id::bigint)
ORDER BY id DESC
LIMIT @row_limit;

-- name: ListDevTenantTargets :many
SELECT
    t.id,
    t.name,
    t.code,
    (
        SELECT count(*)
        FROM feature_flags f
        WHERE f.target_type = 'tenant' AND f.target_id = t.id
    ) AS overrides
FROM tenants t
WHERE t.deleted_at IS NULL
  AND (
    @search::text = ''
    OR t.name ILIKE '%' || @search::text || '%'
    OR t.code ILIKE '%' || @search::text || '%'
  )
ORDER BY t.name ASC, t.id ASC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountDevTenantTargets :one
SELECT count(*)
FROM tenants t
WHERE t.deleted_at IS NULL
  AND (
    @search::text = ''
    OR t.name ILIKE '%' || @search::text || '%'
    OR t.code ILIKE '%' || @search::text || '%'
  );

-- name: CountDevTenantTargetsWithOverrides :one
SELECT count(DISTINCT f.target_id)
FROM feature_flags f
JOIN tenants t ON t.id = f.target_id AND t.deleted_at IS NULL
WHERE f.target_type = 'tenant';

-- name: ListDevUserTargets :many
SELECT
    u.id,
    u.name,
    u.email,
    u.tenant_id,
    t.name AS tenant_name,
    (
        SELECT count(*)
        FROM feature_flags f
        WHERE f.target_type = 'user' AND f.target_id = u.id
    ) AS overrides
FROM users u
LEFT JOIN tenants t ON t.id = u.tenant_id AND t.deleted_at IS NULL
WHERE u.deleted_at IS NULL
  AND (
    @search::text = ''
    OR u.name ILIKE '%' || @search::text || '%'
    OR u.email ILIKE '%' || @search::text || '%'
  )
ORDER BY u.name ASC, u.id ASC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountDevUserTargets :one
SELECT count(*)
FROM users u
WHERE u.deleted_at IS NULL
  AND (
    @search::text = ''
    OR u.name ILIKE '%' || @search::text || '%'
    OR u.email ILIKE '%' || @search::text || '%'
  );

-- name: CountDevUserTargetsWithOverrides :one
SELECT count(DISTINCT f.target_id)
FROM feature_flags f
JOIN users u ON u.id = f.target_id AND u.deleted_at IS NULL
WHERE f.target_type = 'user';

-- name: ListDevDevelopers :many
SELECT
    u.id,
    u.name,
    u.email,
    u.is_active,
    u.last_login_at,
    u.created_at,
    (
        SELECT count(*)
        FROM personal_access_tokens t
        WHERE t.tokenable_type = 'user'
          AND t.tokenable_id = u.id
          AND (t.expires_at IS NULL OR t.expires_at > now())
    ) AS active_tokens
FROM users u
JOIN model_has_roles mr ON mr.model_type = 'user' AND mr.model_id = u.id
JOIN roles r ON r.id = mr.role_id
WHERE u.deleted_at IS NULL
  AND r.name = @role_name
  AND r.tenant_id IS NULL
  AND (
    @search::text = ''
    OR u.name ILIKE '%' || @search::text || '%'
    OR u.email ILIKE '%' || @search::text || '%'
  )
ORDER BY u.id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountDevDevelopers :one
SELECT count(*)
FROM users u
JOIN model_has_roles mr ON mr.model_type = 'user' AND mr.model_id = u.id
JOIN roles r ON r.id = mr.role_id
WHERE u.deleted_at IS NULL
  AND r.name = @role_name
  AND r.tenant_id IS NULL
  AND (
    @search::text = ''
    OR u.name ILIKE '%' || @search::text || '%'
    OR u.email ILIKE '%' || @search::text || '%'
  );

-- name: DevInspectorRoles :many
-- Platform roles (no tenant) with their permission and user counts.
SELECT r.id, r.name, r.guard_name, r.is_system,
       (SELECT count(*) FROM role_has_permissions rp WHERE rp.role_id = r.id)::bigint AS permission_count,
       (SELECT count(*) FROM model_has_roles mr WHERE mr.role_id = r.id)::bigint AS user_count
FROM roles r
WHERE r.tenant_id IS NULL
ORDER BY r.id;

-- name: DevInspectorTenantRoleCount :one
SELECT count(*) FROM roles WHERE tenant_id IS NOT NULL;

-- name: DevInspectorPermissions :many
SELECT name, "group", description, is_system FROM permissions ORDER BY "group", name;

-- name: FinishDevAction :exec
UPDATE dev_actions
SET status = @status, error_message = @error_message, duration_ms = @duration_ms, params = @params
WHERE id = @id;

-- name: ListDevCommandRuns :many
-- Command runs (dev_actions rows with action 'command.run'), newest first.
SELECT * FROM dev_actions
WHERE action = 'command.run'
  AND (@target::text = '' OR target = @target::text)
  AND (@before_id::bigint = 0 OR id < @before_id::bigint)
ORDER BY id DESC
LIMIT @row_limit;

-- name: DevCommandRunSummary :many
-- Last run and run count per command.
SELECT DISTINCT ON (target) target, status, created_at,
       count(*) OVER (PARTITION BY target)::bigint AS runs
FROM dev_actions
WHERE action = 'command.run' AND target IS NOT NULL
ORDER BY target, id DESC;

-- name: GetDevAction :one
SELECT * FROM dev_actions WHERE id = @id;
