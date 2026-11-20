-- name: InsertDevRequest :one
INSERT INTO dev_requests (request_id, method, path, route, status, duration_ms, request_bytes,
                          response_bytes, user_id, impersonator_id, tenant_id, branch_id, ip, user_agent,
                          headers, request_body, response_body, created_at)
VALUES (@request_id, @method, @path, @route, @status, @duration_ms, @request_bytes,
        @response_bytes, @user_id, @impersonator_id, @tenant_id, @branch_id, @ip, @user_agent,
        @headers, @request_body, @response_body, @created_at)
RETURNING id;

-- name: InsertDevQuery :exec
INSERT INTO dev_queries (request_id, sql, args, duration_ms, row_count, error, created_at)
VALUES (@request_id, @sql, @args, @duration_ms, @row_count, @error, @created_at);

-- name: InsertDevCacheEvent :exec
INSERT INTO dev_cache_events (request_id, operation, key, hit, duration_ms, created_at)
VALUES (@request_id, @operation, @key, @hit, @duration_ms, @created_at);

-- name: InsertDevQueueEvent :exec
INSERT INTO dev_queue_events (request_id, event, queue, kind, job_id, duration_ms, error, created_at)
VALUES (@request_id, @event, @queue, @kind, @job_id, @duration_ms, @error, @created_at);

-- name: InsertDevLog :exec
INSERT INTO dev_logs (request_id, level, message, attributes, created_at)
VALUES (@request_id, @level, @message, @attributes, @created_at);

-- name: InsertDevException :exec
INSERT INTO dev_exceptions (request_id, class, message, stack, created_at)
VALUES (@request_id, @class, @message, @stack, @created_at);

-- name: GetDevRequestByID :one
SELECT * FROM dev_requests WHERE id = @id;

-- name: GetDevLogByID :one
SELECT * FROM dev_logs WHERE id = @id;

-- name: GetDevExceptionByID :one
SELECT * FROM dev_exceptions WHERE id = @id;

-- name: ListDevRequests :many
SELECT * FROM dev_requests
WHERE (@before_id::bigint = 0 OR id < @before_id::bigint)
  AND (@method::text = '' OR method = @method::text)
  AND (@status::integer = 0 OR status = @status::integer)
  AND (@search::text = '' OR path ILIKE '%' || @search::text || '%')
  AND (@tenant_id::bigint = 0 OR tenant_id = @tenant_id::bigint)
  AND (@user_id::bigint = 0 OR user_id = @user_id::bigint)
  AND (@request_id::text = '' OR request_id = @request_id::text)
ORDER BY id DESC
LIMIT @row_limit;

-- name: ListDevQueriesByRequestID :many
SELECT * FROM dev_queries WHERE request_id = @request_id ORDER BY id;

-- name: ListDevCacheEventsByRequestID :many
SELECT * FROM dev_cache_events WHERE request_id = @request_id ORDER BY id;

-- name: ListDevQueueEventsByRequestID :many
SELECT * FROM dev_queue_events WHERE request_id = @request_id ORDER BY id;

-- name: ListDevLogsByRequestID :many
SELECT * FROM dev_logs WHERE request_id = @request_id ORDER BY id;

-- name: ListDevExceptionsByRequestID :many
SELECT * FROM dev_exceptions WHERE request_id = @request_id ORDER BY id;

-- name: ListDevLogs :many
SELECT * FROM dev_logs
WHERE (@before_id::bigint = 0 OR id < @before_id::bigint)
  AND (@level::text = '' OR level = @level::text)
  AND (@request_id::text = '' OR request_id = @request_id::text)
  AND (@search::text = '' OR message ILIKE '%' || @search::text || '%'
       OR coalesce(attributes::text, '') ILIKE '%' || @search::text || '%'
       OR coalesce(request_id, '') ILIKE @search::text || '%')
ORDER BY id DESC
LIMIT @row_limit;

-- name: ListDevExceptions :many
SELECT * FROM dev_exceptions
WHERE (@before_id::bigint = 0 OR id < @before_id::bigint)
  AND (@request_id::text = '' OR request_id = @request_id::text)
ORDER BY id DESC
LIMIT @row_limit;

-- name: PruneDevRequests :execrows
DELETE FROM dev_requests WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: PruneDevQueries :execrows
DELETE FROM dev_queries WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: PruneDevCacheEvents :execrows
DELETE FROM dev_cache_events WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: PruneDevQueueEvents :execrows
DELETE FROM dev_queue_events WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: PruneDevLogs :execrows
DELETE FROM dev_logs WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: PruneDevExceptions :execrows
DELETE FROM dev_exceptions WHERE created_at < now() - make_interval(days => @retention_days::int);
