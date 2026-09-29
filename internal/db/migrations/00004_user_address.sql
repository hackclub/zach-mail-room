-- +goose Up
-- Primary shipping address from Hack Club Auth (address scope), used to
-- prefill request forms. Refreshed on every sign-in that returns one.
ALTER TABLE users ADD COLUMN address JSONB;

-- +goose Down
ALTER TABLE users DROP COLUMN address;
