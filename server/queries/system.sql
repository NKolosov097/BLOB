-- name: EnqueueJob :one
INSERT INTO jobs (id, kind, payload, status, available_at)
VALUES ($1, $2, $3, 'pending', $4)
RETURNING *;

-- name: ClaimJob :one
WITH candidate AS (
    SELECT id FROM jobs
    WHERE (status = 'pending' AND available_at <= now())
       OR (status = 'running' AND locked_at < now() - interval '5 minutes')
    ORDER BY available_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE jobs
SET status = 'running', attempts = attempts + 1, locked_at = now(), updated_at = now()
WHERE id = (SELECT id FROM candidate)
RETURNING *;

-- name: CompleteJob :exec
UPDATE jobs SET status = 'done', locked_at = NULL, last_error_code = NULL, updated_at = now()
WHERE id = $1 AND status = 'running';

-- name: FailJob :exec
UPDATE jobs
SET status = CASE WHEN attempts >= 5 THEN 'failed' ELSE 'pending' END,
    available_at = CASE WHEN attempts >= 5 THEN available_at ELSE now() + make_interval(secs => LEAST(300, attempts * attempts)) END,
    locked_at = NULL,
    last_error_code = $2,
    updated_at = now()
WHERE id = $1 AND status = 'running';
