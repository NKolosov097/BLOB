-- +goose Up
ALTER TABLE jobs ADD COLUMN claim_token uuid;
-- +goose Down
ALTER TABLE jobs DROP COLUMN claim_token;
