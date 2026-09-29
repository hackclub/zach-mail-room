package store

import (
	"errors"
	"testing"
	"time"

	"github.com/hackclub/zach-mail-room/internal/auth"
	"github.com/hackclub/zach-mail-room/internal/importer"
	"github.com/hackclub/zach-mail-room/internal/swag"
)

func imported(extID, email string) importer.Request {
	created := time.Date(2026, 9, 19, 19, 3, 0, 0, time.UTC)
	return importer.Request{
		Source: "fillout", ExternalID: extID, Email: email, Name: "Orpheus Dino",
		Address: addr,
		Lines: []importer.SKULine{
			{SKU: "Pri/Bok/Mini25/1st", Quantity: 10}, {SKU: "Sti/Bra/O&H/Lap", Quantity: 50}, {SKU: "Pri/Bok/Mini25/1st", Quantity: 5},
		},
		Status: swag.StatusDispatched, AirtableRecordID: "recAAA", InternalNote: "Imported from Fillout", CreatedAt: created,
	}
}

func TestImportRequest(t *testing.T) {
	s := newStore(t)
	names := map[string]string{"Pri/Bok/Mini25/1st": "Mini-magazine"}

	created, err := s.ImportRequest(ctx, imported("sub-1", "orpheus@example.com"), names)
	if err != nil || !created {
		t.Fatalf("ImportRequest = %v, %v", created, err)
	}
	again, err := s.ImportRequest(ctx, imported("sub-1", "orpheus@example.com"), names)
	if err != nil || again {
		t.Fatalf("re-import should be a no-op: %v, %v", again, err)
	}

	all, _ := s.ListRequests(ctx, "")
	if len(all) != 1 {
		t.Fatalf("have %d requests", len(all))
	}
	r := all[0]
	if r.Source != "fillout" || r.AirtableRecordID != "recAAA" || r.Status != swag.StatusDispatched || !r.CreatedAt.Equal(imported("", "").CreatedAt) {
		t.Errorf("request = %+v", r)
	}
	if len(r.Lines) != 2 {
		t.Fatalf("duplicate SKU lines should merge: %+v", r.Lines)
	}
	for _, l := range r.Lines {
		if l.SKU == "Pri/Bok/Mini25/1st" && (l.Quantity != 15 || l.Name != "Mini-magazine") {
			t.Errorf("magazine line = %+v", l)
		}
		if l.SKU == "Sti/Bra/O&H/Lap" && l.Name != "Sti/Bra/O&H/Lap" {
			t.Errorf("unknown SKU should be named by SKU: %+v", l)
		}
	}
	items, _ := s.ListItems(ctx, false)
	visible, _ := s.ListItems(ctx, true)
	if len(items) != 2 || len(visible) != 0 {
		t.Errorf("imported SKUs should become hidden items: all=%d visible=%d", len(items), len(visible))
	}
}

func TestImportAttachesToExistingUserAndLoginClaimsPlaceholder(t *testing.T) {
	s := newStore(t)
	existing := mkUser(t, s, "ident!1", "existing@example.com")
	if _, err := s.ImportRequest(ctx, imported("sub-1", "EXISTING@example.com"), nil); err != nil {
		t.Fatal(err)
	}
	mine, _ := s.ListRequestsByUser(ctx, existing.ID)
	if len(mine) != 1 {
		t.Fatalf("import should attach to the existing account; got %d", len(mine))
	}

	if _, err := s.ImportRequest(ctx, imported("sub-2", "new@example.com"), nil); err != nil {
		t.Fatal(err)
	}
	u, err := s.UpsertUser(ctx, auth.Identity{ID: "ident!new", Email: "new@example.com", Name: "New Person"})
	if err != nil {
		t.Fatal(err)
	}
	mine, _ = s.ListRequestsByUser(ctx, u.ID)
	if len(mine) != 1 || u.HCAID != "ident!new" || u.Name != "New Person" {
		t.Fatalf("first login should claim the imported placeholder: user=%+v requests=%d", u, len(mine))
	}

	// Imported history counts toward limits and cooldowns.
	it := mkItem(t, s, "Sti/A", true)
	var last *time.Time
	s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusPending},
		func(_ map[int64]int, l *time.Time) error { last = l; return errors.New("stop") })
	if last == nil {
		t.Error("imported request should count as the user's last request")
	}
}

func TestSetTrackingByAirtableID(t *testing.T) {
	s := newStore(t)
	s.ImportRequest(ctx, imported("sub-1", "a@example.com"), nil)
	mailed := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	n, err := s.SetTrackingByAirtableID(ctx, "recAAA", "9400111", "USPS", &mailed)
	if err != nil || n != 1 {
		t.Fatalf("SetTrackingByAirtableID = %d, %v", n, err)
	}
	if n, _ := s.SetTrackingByAirtableID(ctx, "recNOPE", "x", "y", nil); n != 0 {
		t.Errorf("unknown record updated %d rows", n)
	}
	all, _ := s.ListRequests(ctx, "")
	if all[0].TrackingNumber != "9400111" || all[0].Carrier != "USPS" || all[0].MailedAt == nil || !all[0].MailedAt.Equal(mailed) {
		t.Fatalf("request = %+v", all[0])
	}
}
