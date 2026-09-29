-- +goose Up
-- Admin-only notes (e.g. the old Airtable internal_notes with payment links).
-- admin_note stays the requester-visible note (e.g. a rejection reason).
ALTER TABLE swag_requests ADD COLUMN internal_note TEXT NOT NULL DEFAULT '';
UPDATE swag_requests SET internal_note = admin_note, admin_note = '' WHERE source <> 'app';

-- +goose Down
UPDATE swag_requests SET admin_note = internal_note WHERE source <> 'app';
ALTER TABLE swag_requests DROP COLUMN internal_note;
