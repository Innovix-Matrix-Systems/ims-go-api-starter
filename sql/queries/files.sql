-- name: CreateFile :one
INSERT INTO files (
    uuid, name, file_name, disk, path, mime_type, size, type,
    owner_type, owner_id, collection, created_by, updated_by
)
VALUES (
    @uuid, @name, @file_name, @disk, @path, @mime_type, @size, @type,
    @owner_type, @owner_id, @collection, @created_by, @updated_by
)
RETURNING *;

-- name: GetFile :one
SELECT * FROM files WHERE id = @id;

-- name: GetFileByOwner :one
SELECT * FROM files
WHERE owner_type = @owner_type::text
  AND owner_id = @owner_id::bigint
  AND collection = @collection::text
ORDER BY id DESC
LIMIT 1;

-- name: ListFiles :many
SELECT * FROM files
WHERE (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type')::text)
  AND (sqlc.narg('attached')::boolean IS NULL OR (owner_id IS NOT NULL) = sqlc.narg('attached')::boolean)
ORDER BY created_at DESC, id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountFiles :one
SELECT count(*) FROM files
WHERE (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type')::text)
  AND (sqlc.narg('attached')::boolean IS NULL OR (owner_id IS NOT NULL) = sqlc.narg('attached')::boolean);

-- name: DeleteFile :execrows
DELETE FROM files WHERE id = @id;

-- name: DeleteFilesByOwner :many
DELETE FROM files
WHERE owner_type = @owner_type::text
  AND owner_id = @owner_id::bigint
  AND collection = @collection::text
RETURNING *;

-- name: ListProfileFilesByUserIDs :many
SELECT * FROM files
WHERE owner_type = 'user' AND collection = 'profile'
  AND owner_id = ANY(@user_ids::bigint[]);
