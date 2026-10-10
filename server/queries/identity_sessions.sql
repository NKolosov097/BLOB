-- name: LockIdentityUser :one
SELECT * FROM users WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: GetDeviceByBinding :one
SELECT * FROM devices
WHERE user_id = sqlc.arg(user_id) AND binding_hash = sqlc.arg(binding_hash) AND revoked_at IS NULL
FOR UPDATE;

-- name: CountActiveDevices :one
SELECT count(*) FROM devices WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- name: InsertDevice :one
INSERT INTO devices (id, user_id, binding_hash, browser, os, created_at, last_seen_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(binding_hash), sqlc.arg(browser), sqlc.arg(os), sqlc.arg(now), sqlc.arg(now))
RETURNING *;

-- name: TouchDevice :exec
UPDATE devices SET last_seen_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: InsertSession :one
INSERT INTO sessions (id, user_id, device_id, token_hash, created_at, last_seen_at, idle_expires_at, absolute_expires_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(device_id), sqlc.arg(token_hash), sqlc.arg(now), sqlc.arg(now), sqlc.arg(idle_expires_at), sqlc.arg(absolute_expires_at))
RETURNING *;

-- name: FindSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = sqlc.arg(token_hash);

-- name: AuthenticateSession :one
UPDATE sessions s
SET last_seen_at = sqlc.arg(now),
    idle_expires_at = LEAST(sqlc.arg(new_idle_expires_at), s.absolute_expires_at)
FROM users u, devices d
WHERE s.token_hash = sqlc.arg(token_hash)
  AND u.id = s.user_id
  AND d.id = s.device_id AND d.user_id = s.user_id
  AND u.access_state = 'active'
  AND d.revoked_at IS NULL
  AND s.revoked_at IS NULL
  AND s.idle_expires_at > sqlc.arg(now)
  AND s.absolute_expires_at > sqlc.arg(now)
RETURNING s.id, s.user_id, s.device_id, s.created_at, s.last_seen_at, s.idle_expires_at, s.absolute_expires_at,
    u.access_state, u.mfa_required, u.security_version;

-- name: LockActiveSession :one
SELECT s.* FROM sessions s
JOIN devices d ON d.id = s.device_id AND d.user_id = s.user_id
WHERE s.token_hash = sqlc.arg(token_hash) AND s.user_id = sqlc.arg(user_id)
  AND s.revoked_at IS NULL AND d.revoked_at IS NULL
  AND s.idle_expires_at > sqlc.arg(now) AND s.absolute_expires_at > sqlc.arg(now)
FOR UPDATE OF s;

-- name: RotateSessionToken :one
UPDATE sessions SET token_hash = sqlc.arg(token_hash), last_seen_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListActiveSessions :many
SELECT * FROM sessions
WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL
  AND idle_expires_at > sqlc.arg(now) AND absolute_expires_at > sqlc.arg(now)
  AND (created_at < sqlc.arg(after_created_at)
       OR (created_at = sqlc.arg(after_created_at) AND id < sqlc.arg(after_id)))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: ListDevices :many
SELECT * FROM devices
WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: FindOwnedSession :one
SELECT * FROM sessions WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) FOR UPDATE;

-- name: FindOwnedDevice :one
SELECT * FROM devices WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) FOR UPDATE;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = sqlc.arg(now) WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeDevice :exec
UPDATE devices SET revoked_at = sqlc.arg(now) WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeDeviceSessions :exec
UPDATE sessions SET revoked_at = sqlc.arg(now) WHERE device_id = sqlc.arg(device_id) AND revoked_at IS NULL;

-- name: RevokeAllSessions :exec
UPDATE sessions SET revoked_at = sqlc.arg(now) WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL;
