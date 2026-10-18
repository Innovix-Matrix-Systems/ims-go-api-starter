-- name: CreateToken :one
INSERT INTO personal_access_tokens (tokenable_type, tokenable_id, name, device, token, abilities, expires_at)
VALUES (@tokenable_type, @tokenable_id, @name, @device, @token, @abilities, @expires_at)
RETURNING *;

-- name: GetToken :one
SELECT * FROM personal_access_tokens
WHERE id = @id;

-- name: TouchToken :execrows
UPDATE personal_access_tokens
SET last_used_at = @last_used_at, updated_at = now()
WHERE id = @id;

-- name: DeleteTokenByDevice :execrows
DELETE FROM personal_access_tokens
WHERE tokenable_type = @tokenable_type
  AND tokenable_id = @tokenable_id
  AND device = @device;

-- name: DeleteTokensForUser :execrows
DELETE FROM personal_access_tokens
WHERE tokenable_type = @tokenable_type
  AND tokenable_id = @tokenable_id;
