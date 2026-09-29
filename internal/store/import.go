package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hackclub/zach-mail-room/internal/importer"
)

// ImportRequest stores one historical request. It is idempotent on
// (Source, ExternalID) and returns false if the request was already imported.
// Limits are not checked: this is history, not a new request. The person is
// matched to an existing user by email, or a placeholder user is created for
// them to claim on first sign-in. SKUs not yet in the catalog become hidden
// items named from itemNames (sku -> name), falling back to the SKU.
func (s *Store) ImportRequest(ctx context.Context, in importer.Request, itemNames map[string]string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM swag_requests WHERE source = $1 AND external_id = $2)`,
		in.Source, in.ExternalID).Scan(&exists); err != nil || exists {
		return false, err
	}

	var userID int64
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE lower(email) = lower($1) ORDER BY (hca_id IS NULL), id LIMIT 1`, in.Email).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO users (email, name) VALUES (lower($1), $2) RETURNING id`, in.Email, in.Name).Scan(&userID)
	}
	if err != nil {
		return false, err
	}

	qty := map[int64]int{}
	var order []int64
	for _, l := range in.Lines {
		name := itemNames[l.SKU]
		if name == "" {
			name = l.SKU
		}
		var itemID int64
		err := tx.QueryRow(ctx, `
			WITH ins AS (
				INSERT INTO items (sku, name, visible, max_per_request) VALUES ($1, $2, FALSE, 1)
				ON CONFLICT (sku) DO NOTHING RETURNING id
			)
			SELECT id FROM ins UNION ALL SELECT id FROM items WHERE sku = $1 LIMIT 1`, l.SKU, name).Scan(&itemID)
		if err != nil {
			return false, err
		}
		if _, seen := qty[itemID]; !seen {
			order = append(order, itemID)
		}
		qty[itemID] += l.Quantity
	}

	a := in.Address
	var reqID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO swag_requests (user_id, status, first_name, last_name, line_1, line_2, city, state, postal_code, country,
			phone_number, shipping_fee_cents, paid_at, internal_note, source, external_id, airtable_record_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $18) RETURNING id`,
		userID, in.Status, a.FirstName, a.LastName, a.Line1, a.Line2, a.City, a.State, a.PostalCode, a.Country,
		a.Phone, in.ShippingFeeCents, in.PaidAt, in.InternalNote, in.Source, in.ExternalID, in.AirtableRecordID, in.CreatedAt).Scan(&reqID)
	if err != nil {
		return false, mapErr(err)
	}
	for _, itemID := range order {
		if _, err := tx.Exec(ctx, `INSERT INTO swag_request_items (request_id, item_id, quantity) VALUES ($1, $2, $3)`,
			reqID, itemID, qty[itemID]); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

// SetTrackingByAirtableID backfills tracking for requests fulfilled through the
// Airtable warehouse base (Zenventory order_number == Airtable record id).
func (s *Store) SetTrackingByAirtableID(ctx context.Context, recordID, tracking, carrier string, mailedAt *time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE swag_requests SET tracking_number = $2, carrier = $3, mailed_at = coalesce($4, mailed_at), updated_at = now()
		WHERE airtable_record_id = $1 AND airtable_record_id <> ''`, recordID, tracking, carrier, mailedAt)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
