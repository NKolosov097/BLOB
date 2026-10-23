-- name: EnqueueJob :one
INSERT INTO jobs (id, kind, payload, status, available_at)
VALUES ($1, $2, $3, 'pending', $4)
RETURNING *;

-- name: ExpireJobs :exec
UPDATE jobs
SET status = 'failed', locked_at = NULL, claim_token = NULL, last_error_code = 'lease_expired', updated_at = now()
WHERE id IN (
    SELECT id FROM jobs
    WHERE status = 'running' AND attempts >= 5 AND locked_at < now() - interval '5 minutes'
    FOR UPDATE SKIP LOCKED
);

-- name: ClaimJob :one
WITH candidate AS (
    SELECT id FROM jobs
    WHERE attempts < 5 AND (
        (status = 'pending' AND available_at <= now())
        OR (status = 'running' AND locked_at < now() - interval '5 minutes')
    )
    ORDER BY available_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE jobs
SET status = 'running', attempts = attempts + 1, locked_at = now(), claim_token = sqlc.arg(claim_token), updated_at = now()
WHERE id = (SELECT id FROM candidate)
RETURNING *;

-- name: CompleteJob :execrows
UPDATE jobs SET status = 'done', locked_at = NULL, claim_token = NULL, last_error_code = NULL, updated_at = now()
WHERE id = sqlc.arg(id) AND claim_token = sqlc.arg(claim_token) AND status = 'running';

-- name: FailJob :execrows
UPDATE jobs
SET status = CASE WHEN attempts >= 5 THEN 'failed' ELSE 'pending' END,
    available_at = CASE WHEN attempts >= 5 THEN available_at ELSE now() + make_interval(secs => LEAST(300, attempts * attempts)) END,
    locked_at = NULL,
    claim_token = NULL,
    last_error_code = sqlc.arg(last_error_code),
    updated_at = now()
WHERE id = sqlc.arg(id) AND claim_token = sqlc.arg(claim_token) AND status = 'running';
