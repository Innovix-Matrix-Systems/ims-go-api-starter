-- name: GetUser :one
SELECT * FROM users
WHERE id = @id AND deleted_at IS NULL
  AND (@all_tenants::boolean OR tenant_id = @tenant_id::bigint);

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE lower(email) = lower(@email) AND deleted_at IS NULL;

-- name: ListUsers :many
SELECT * FROM users
WHERE deleted_at IS NULL
  AND (@all_tenants::boolean OR users.tenant_id = @tenant_id)
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR COALESCE(phone, '') ILIKE '%' || sqlc.narg('search')::text || '%'
  )
  AND (sqlc.narg('is_active')::smallint IS NULL OR is_active = sqlc.narg('is_active')::smallint)
  AND (
    sqlc.narg('role_name')::text IS NULL
    OR EXISTS (
      SELECT 1 FROM model_has_roles mr
      JOIN roles r ON r.id = mr.role_id
      WHERE mr.model_type = 'user' AND mr.model_id = users.id
        AND r.name = sqlc.narg('role_name')::text
    )
  )
ORDER BY
  CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'asc' THEN id END ASC,
  CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'desc' THEN id END DESC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN name END ASC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'desc' THEN name END DESC,
  CASE WHEN @order_by::text = 'email' AND @order_dir::text = 'asc' THEN lower(email) END ASC,
  CASE WHEN @order_by::text = 'email' AND @order_dir::text = 'desc' THEN lower(email) END DESC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'asc' THEN created_at END ASC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'desc' THEN created_at END DESC,
  id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountUsers :one
SELECT count(*) FROM users
WHERE deleted_at IS NULL
  AND (@all_tenants::boolean OR users.tenant_id = @tenant_id)
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR COALESCE(phone, '') ILIKE '%' || sqlc.narg('search')::text || '%'
  )
  AND (sqlc.narg('is_active')::smallint IS NULL OR is_active = sqlc.narg('is_active')::smallint)
  AND (
    sqlc.narg('role_name')::text IS NULL
    OR EXISTS (
      SELECT 1 FROM model_has_roles mr
      JOIN roles r ON r.id = mr.role_id
      WHERE mr.model_type = 'user' AND mr.model_id = users.id
        AND r.name = sqlc.narg('role_name')::text
    )
  );

-- name: CreateUser :one
INSERT INTO users (
    tenant_id, branch_id, active_branch_id, first_name, last_name, user_name,
    name, email, email_verified_at, phone, password, is_active, created_by, updated_by
)
VALUES (
    @tenant_id, @branch_id, @active_branch_id, @first_name, @last_name, @user_name,
    @name, @email, @email_verified_at, @phone, @password, @is_active, @created_by, @updated_by
)
RETURNING *;

-- name: UpdateUser :one
UPDATE users
SET first_name  = @first_name,
    last_name   = @last_name,
    name        = @name,
    phone       = @phone,
    is_active   = @is_active,
    branch_id   = @branch_id,
    updated_by  = @updated_by,
    updated_at  = now()
WHERE id = @id AND deleted_at IS NULL
  AND (@all_tenants::boolean OR tenant_id = @tenant_id::bigint)
  -- Optional optimistic lock: when the client sends the updated_at it last
  -- saw, a concurrent change makes this match nothing (409 in the service).
  AND (sqlc.narg('expected_updated_at')::timestamptz IS NULL
       OR updated_at = sqlc.narg('expected_updated_at')::timestamptz)
RETURNING *;

-- name: SoftDeleteUser :execrows
UPDATE users
SET deleted_at = now(), updated_by = @updated_by, updated_at = now()
WHERE id = @id AND deleted_at IS NULL
  AND (@all_tenants::boolean OR tenant_id = @tenant_id::bigint);

-- name: UpdateUserPassword :execrows
UPDATE users
SET password = @password, updated_by = @updated_by, updated_at = now()
WHERE id = @id AND deleted_at IS NULL
  AND (@all_tenants::boolean OR tenant_id = @tenant_id::bigint);

-- name: TouchUserLogin :execrows
UPDATE users
SET last_login_at = now(), last_active_device = @device, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: SwitchUserBranch :execrows
UPDATE users
SET active_branch_id = @branch_id, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: EmailExists :one
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE lower(email) = lower(@email)
      AND deleted_at IS NULL
      AND (@exclude_id::bigint IS NULL OR id <> @exclude_id::bigint)
);

-- name: PhoneExists :one
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE phone = @phone
      AND deleted_at IS NULL
      AND (@exclude_id::bigint IS NULL OR id <> @exclude_id::bigint)
);

-- name: ListUsersForExport :many
SELECT u.id, u.name, u.email, u.phone, u.is_active, u.branch_id, u.created_at,
       COALESCE(b.name, '')::text AS branch_name,
       COALESCE((
           SELECT string_agg(r.name, ', ' ORDER BY r.name)
           FROM model_has_roles mr
           JOIN roles r ON r.id = mr.role_id
           WHERE mr.model_type = 'user' AND mr.model_id = u.id
       ), '')::text AS role_names
FROM users u
LEFT JOIN branches b ON b.id = u.branch_id
WHERE u.deleted_at IS NULL
  AND (@all_tenants::boolean OR u.tenant_id = @tenant_id::bigint)
  AND (
      sqlc.narg('search')::text IS NULL
      OR u.name ILIKE '%' || sqlc.narg('search') || '%'
      OR u.email ILIKE '%' || sqlc.narg('search') || '%'
      OR u.phone ILIKE '%' || sqlc.narg('search') || '%'
  )
  AND (sqlc.narg('is_active')::boolean IS NULL OR (u.is_active = 1) = sqlc.narg('is_active')::boolean)
  AND (
      sqlc.narg('role_name')::text IS NULL
      OR EXISTS (
          SELECT 1 FROM model_has_roles mr2
          JOIN roles r2 ON r2.id = mr2.role_id
          WHERE mr2.model_type = 'user' AND mr2.model_id = u.id AND r2.name = sqlc.narg('role_name')
      )
  )
ORDER BY
  CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'asc' THEN u.id END ASC,
  CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'desc' THEN u.id END DESC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN lower(u.name) END ASC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'desc' THEN lower(u.name) END DESC,
  CASE WHEN @order_by::text = 'email' AND @order_dir::text = 'asc' THEN lower(u.email) END ASC,
  CASE WHEN @order_by::text = 'email' AND @order_dir::text = 'desc' THEN lower(u.email) END DESC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'asc' THEN u.created_at END ASC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'desc' THEN u.created_at END DESC,
  u.id DESC
LIMIT @page_limit OFFSET @page_offset;
-- name: MarkUserEmailVerified :execrows
UPDATE users
SET email_verified_at = COALESCE(email_verified_at, now()), updated_at = now()
WHERE id = @id AND deleted_at IS NULL;
