-- +goose Up
CREATE TABLE jobs (
    id uuid PRIMARY KEY,
    kind text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'running', 'done', 'failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    last_error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX jobs_claim_idx ON jobs (available_at, created_at) WHERE status = 'pending';
CREATE INDEX jobs_reclaim_idx ON jobs (locked_at) WHERE status = 'running';
-- +goose Down
DROP TABLE jobs;
