-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY,
    access_state text NOT NULL CHECK (access_state IN ('pending_email', 'active', 'restricted', 'deletion_pending')),
    mfa_required boolean NOT NULL DEFAULT false,
    protection_state text NOT NULL CHECK (protection_state IN ('never_enrolled', 'protected', 'recovery_required')) DEFAULT 'never_enrolled',
    security_version bigint NOT NULL DEFAULT 1 CHECK (security_version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE devices (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id),
    binding_hash bytea NOT NULL CHECK (octet_length(binding_hash) = 32),
    browser text NOT NULL CHECK (browser IN ('Chrome', 'Edge', 'Firefox', 'Safari', 'Other')),
    os text NOT NULL CHECK (os IN ('Windows', 'macOS', 'Linux', 'Android', 'iOS', 'Other')),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    UNIQUE (user_id, binding_hash),
    UNIQUE (id, user_id)
);

CREATE INDEX devices_user_active_idx ON devices (user_id, created_at, id);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id),
    device_id uuid NOT NULL,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    FOREIGN KEY (device_id, user_id) REFERENCES devices (id, user_id)
);

CREATE INDEX sessions_user_active_idx ON sessions (user_id, created_at, id);
CREATE INDEX sessions_device_idx ON sessions (device_id);
-- +goose Down
DROP TABLE sessions;
DROP TABLE devices;
DROP TABLE users;
