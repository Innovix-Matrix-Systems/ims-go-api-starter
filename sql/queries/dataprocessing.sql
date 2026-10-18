-- name: CreateDataProcessingJob :one
INSERT INTO data_processing_jobs (
    tenant_id, branch_id, job_id, type, status, entity_type,
    filters, total_items, user_id
)
VALUES (
    @tenant_id, @branch_id, @job_id, @type, @status, @entity_type,
    @filters, @total_items, @user_id
)
RETURNING *;

-- name: GetDataProcessingJob :one
SELECT * FROM data_processing_jobs
WHERE id = @id;

-- name: GetDataProcessingJobByJobID :one
SELECT * FROM data_processing_jobs
WHERE job_id = @job_id;

-- name: ListDataProcessingJobs :many
SELECT * FROM data_processing_jobs
WHERE (@all_tenants::boolean OR tenant_id = @tenant_id)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type')::text)
  AND (sqlc.narg('user_id')::bigint IS NULL OR user_id = sqlc.narg('user_id')::bigint)
  AND (
      @search::text IS NULL
      OR job_id ILIKE '%' || @search || '%'
      OR file_name ILIKE '%' || @search || '%'
      OR entity_type ILIKE '%' || @search || '%'
  )
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountDataProcessingJobs :one
SELECT count(*) FROM data_processing_jobs
WHERE (@all_tenants::boolean OR tenant_id = @tenant_id)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type')::text)
  AND (sqlc.narg('user_id')::bigint IS NULL OR user_id = sqlc.narg('user_id')::bigint)
  AND (
      @search::text IS NULL
      OR job_id ILIKE '%' || @search || '%'
      OR file_name ILIKE '%' || @search || '%'
      OR entity_type ILIKE '%' || @search || '%'
  );

-- name: UpdateDataProcessingJobStatus :one
UPDATE data_processing_jobs
SET status         = @status,
    error_message  = @error_message,
    started_at     = CASE WHEN @status = 'processing' AND started_at IS NULL THEN now() ELSE started_at END,
    completed_at   = CASE WHEN @status IN ('completed', 'failed') THEN now() ELSE completed_at END,
    updated_at     = now()
WHERE id = @id
RETURNING *;

-- name: UpdateDataProcessingJobProgress :execrows
UPDATE data_processing_jobs
SET total_items     = @total_items,
    processed_items = @processed_items,
    success_count   = @success_count,
    error_count     = @error_count,
    errors          = @errors,
    updated_at      = now()
WHERE id = @id;

-- name: UpdateDataProcessingJobFile :execrows
UPDATE data_processing_jobs
SET file_name = @file_name,
    file_disk = @file_disk,
    file_path = @file_path,
    file_size = @file_size,
    file_mime = @file_mime,
    updated_at = now()
WHERE id = @id;

-- name: DeleteCompletedDataProcessingJobs :execrows
DELETE FROM data_processing_jobs
WHERE status IN ('completed', 'failed')
  AND completed_at IS NOT NULL
  AND completed_at < now() - make_interval(days => @retention_days::int);

-- name: CountCompletedDataProcessingJobsBefore :one
SELECT count(*) FROM data_processing_jobs
WHERE status IN ('completed', 'failed')
  AND completed_at IS NOT NULL
  AND completed_at < now() - make_interval(days => @retention_days::int);

-- name: ListExpiredDataProcessingJobs :many
SELECT id, job_id, status, file_disk, file_path, file_name
FROM data_processing_jobs
WHERE status IN ('completed', 'failed')
  AND completed_at IS NOT NULL
  AND completed_at < now() - make_interval(days => @retention_days::int)
ORDER BY id
LIMIT @page_limit;

-- name: DeleteDataProcessingJobsByIDs :execrows
DELETE FROM data_processing_jobs
WHERE id = ANY(@ids::bigint[]);
