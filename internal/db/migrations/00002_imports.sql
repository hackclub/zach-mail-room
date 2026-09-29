-- +goose Up
-- People imported from historical requests have no Hack Club Auth id until
-- they first sign in, at which point UpsertUser claims the row by email.
ALTER TABLE users ALTER COLUMN hca_id DROP NOT NULL;
CREATE UNIQUE INDEX users_unclaimed_email_idx ON users (lower(email)) WHERE hca_id IS NULL;

ALTER TABLE swag_requests
    ADD COLUMN source             TEXT NOT NULL DEFAULT 'app',
    ADD COLUMN external_id        TEXT,
    ADD COLUMN airtable_record_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN mailed_at          TIMESTAMPTZ;
CREATE UNIQUE INDEX swag_requests_source_external_idx ON swag_requests (source, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX swag_requests_airtable_idx ON swag_requests (airtable_record_id) WHERE airtable_record_id <> '';

-- +goose Down
DROP INDEX swag_requests_airtable_idx;
DROP INDEX swag_requests_source_external_idx;
ALTER TABLE swag_requests DROP COLUMN mailed_at, DROP COLUMN airtable_record_id, DROP COLUMN external_id, DROP COLUMN source;
DROP INDEX users_unclaimed_email_idx;
DELETE FROM users WHERE hca_id IS NULL;
ALTER TABLE users ALTER COLUMN hca_id SET NOT NULL;
