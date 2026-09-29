package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hackclub/zach-mail-room/internal/auth"
	"github.com/hackclub/zach-mail-room/internal/db/dbtest"
	"github.com/hackclub/zach-mail-room/internal/swag"
)

var ctx = context.Background()

func newStore(t *testing.T) *Store { return New(dbtest.New(t)) }

func mkUser(t *testing.T, s *Store, hcaID, email string) *User {
	t.Helper()
	u, err := s.UpsertUser(ctx, auth.Identity{ID: hcaID, Email: email, Name: "Test " + hcaID})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mkItem(t *testing.T, s *Store, sku string, visible bool) swag.Item {
	t.Helper()
	it, err := s.CreateItem(ctx, swag.Item{SKU: sku, Name: sku, Visible: visible, MaxPerRequest: 2})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

var addr = swag.Address{FirstName: "O", LastName: "D", Line1: "1 Main", City: "Burlington", State: "VT", PostalCode: "05401", Country: "US"}

func TestUpsertUser(t *testing.T) {
	s := newStore(t)
	u1 := mkUser(t, s, "ident!1", "a@x.com")
	u2, err := s.UpsertUser(ctx, auth.Identity{ID: "ident!1", Email: "new@x.com", Name: "Renamed", SlackID: "U9"})
	if err != nil {
		t.Fatal(err)
	}
	if u1.ID != u2.ID || u2.Email != "new@x.com" || u2.Name != "Renamed" || u2.SlackID != "U9" {
		t.Fatalf("upsert did not update in place: %+v -> %+v", u1, u2)
	}
}

func TestSessions(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	tok, err := s.CreateSession(ctx, u.ID, time.Hour)
	if err != nil || len(tok) < 32 {
		t.Fatalf("token %q err %v", tok, err)
	}
	got, err := s.UserBySession(ctx, tok)
	if err != nil || got.ID != u.ID {
		t.Fatalf("UserBySession = %+v, %v", got, err)
	}
	if _, err := s.UserBySession(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown token err = %v", err)
	}
	if err := s.DeleteSession(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserBySession(ctx, tok); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted token err = %v", err)
	}
	expired, _ := s.CreateSession(ctx, u.ID, -time.Minute)
	if _, err := s.UserBySession(ctx, expired); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired token err = %v", err)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	s := newStore(t)
	got, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got.RequestsOpen || got.MaxItemsPerRequest != 5 || got.RequestCooldownDays != 30 || got.InternationalShippingFeeCents != 1500 {
		t.Fatalf("defaults = %+v", got)
	}
	want := swag.Settings{RequestsOpen: false, MaxItemsPerRequest: 3, RequestCooldownDays: 7, InternationalShippingFeeCents: 999}
	if err := s.UpdateSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSettings(ctx)
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestItems(t *testing.T) {
	s := newStore(t)
	a := mkItem(t, s, "Sti/A", true)
	mkItem(t, s, "Sti/B", false)
	if _, err := s.CreateItem(ctx, swag.Item{SKU: "Sti/A", Name: "dup", MaxPerRequest: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate sku err = %v", err)
	}

	visible, _ := s.ListItems(ctx, true)
	all, _ := s.ListItems(ctx, false)
	if len(visible) != 1 || len(all) != 2 {
		t.Fatalf("visible=%d all=%d", len(visible), len(all))
	}

	five := 5
	a.Name, a.MaxPerUser, a.SortOrder = "Renamed", &five, 3
	upd, err := s.UpdateItem(ctx, a)
	if err != nil || upd.Name != "Renamed" || upd.MaxPerUser == nil || *upd.MaxPerUser != 5 {
		t.Fatalf("UpdateItem = %+v, %v", upd, err)
	}
	if _, err := s.UpdateItem(ctx, swag.Item{ID: 999, SKU: "x", Name: "x", MaxPerRequest: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item err = %v", err)
	}
}

func TestCreateRequestPassesUsageToValidator(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	it := mkItem(t, s, "Sti/A", true)

	r1, err := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 2}}, Status: swag.StatusPending},
		func(prior map[int64]int, last *time.Time) error {
			if len(prior) != 0 || last != nil {
				t.Errorf("first request saw prior=%v last=%v", prior, last)
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != swag.StatusPending || len(r1.Lines) != 1 || r1.Lines[0].SKU != "Sti/A" || r1.Lines[0].Quantity != 2 {
		t.Fatalf("request = %+v", r1)
	}

	var sawPrior map[int64]int
	var sawLast *time.Time
	boom := errors.New("rejected by rules")
	_, err = s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusPending},
		func(prior map[int64]int, last *time.Time) error {
			sawPrior, sawLast = prior, last
			return boom
		})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if sawPrior[it.ID] != 2 || sawLast == nil {
		t.Fatalf("prior=%v last=%v", sawPrior, sawLast)
	}
	mine, _ := s.ListRequestsByUser(ctx, u.ID)
	if len(mine) != 1 {
		t.Fatalf("validator error must roll back; have %d requests", len(mine))
	}

	// Rejected requests don't count toward usage.
	if _, err := s.TransitionRequest(ctx, r1.ID, swag.StatusRejected, "nope"); err != nil {
		t.Fatal(err)
	}
	s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusPending},
		func(prior map[int64]int, last *time.Time) error {
			if prior[it.ID] != 0 || last != nil {
				t.Errorf("rejected request counted: prior=%v last=%v", prior, last)
			}
			return nil
		})
}

func TestTransitionRequest(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	it := mkItem(t, s, "Sti/A", true)
	r, _ := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusAwaitingPayment, ShippingFeeCents: 1500}, noCheck)

	if _, err := s.TransitionRequest(ctx, r.ID, swag.StatusDispatched, ""); !errors.Is(err, ErrBadTransition) {
		t.Fatalf("awaiting_payment -> dispatched err = %v", err)
	}
	got, err := s.MarkPaid(ctx, r.ID)
	if err != nil || got.Status != swag.StatusPending || got.PaidAt == nil {
		t.Fatalf("MarkPaid = %+v, %v", got, err)
	}
	got, err = s.MarkDispatched(ctx, r.ID, "pkg!1")
	if err != nil || got.Status != swag.StatusDispatched || got.TheseusOrderID != "pkg!1" {
		t.Fatalf("MarkDispatched = %+v, %v", got, err)
	}
	if _, err := s.MarkDispatched(ctx, r.ID, "pkg!2"); !errors.Is(err, ErrBadTransition) {
		t.Fatalf("double dispatch err = %v", err)
	}
	got, _ = s.SetTracking(ctx, r.ID, "9400", "USPS")
	if got.TrackingNumber != "9400" || got.Carrier != "USPS" {
		t.Fatalf("tracking = %+v", got)
	}
	all, _ := s.ListRequests(ctx, "")
	disp, _ := s.ListRequests(ctx, swag.StatusDispatched)
	pend, _ := s.ListRequests(ctx, swag.StatusPending)
	if len(all) != 1 || len(disp) != 1 || len(pend) != 0 {
		t.Fatalf("all=%d dispatched=%d pending=%d", len(all), len(disp), len(pend))
	}
	if all[0].UserEmail != "a@x.com" {
		t.Errorf("admin listing should include requester email, got %q", all[0].UserEmail)
	}
}

func TestSubmissions(t *testing.T) {
	s := newStore(t)
	a := mkUser(t, s, "ident!1", "author@x.com")
	b := mkUser(t, s, "ident!2", "other@x.com")
	sub, err := s.CreateSubmission(ctx, Submission{UserID: a.ID, Program: "Sprig", Title: "Sprig sticker", Kind: "sticker", FileURL: "https://cdn.hackclub.com/x.png", Quantity: 500})
	if err != nil || sub.Status != "submitted" {
		t.Fatalf("CreateSubmission = %+v, %v", sub, err)
	}
	mine, _ := s.ListSubmissionsByUser(ctx, a.ID)
	theirs, _ := s.ListSubmissionsByUser(ctx, b.ID)
	if len(mine) != 1 || len(theirs) != 0 {
		t.Fatalf("mine=%d theirs=%d", len(mine), len(theirs))
	}
	upd, err := s.UpdateSubmission(ctx, sub.ID, "stocked", "printed 500", "Sti/Spr/1")
	if err != nil || upd.Status != "stocked" || upd.SKU != "Sti/Spr/1" || upd.AdminNote != "printed 500" {
		t.Fatalf("UpdateSubmission = %+v, %v", upd, err)
	}
	if _, err := s.UpdateSubmission(ctx, sub.ID, "bogus", "", ""); err == nil {
		t.Fatal("invalid status should fail")
	}
	all, _ := s.ListSubmissions(ctx)
	if len(all) != 1 || all[0].UserEmail != "author@x.com" {
		t.Fatalf("all = %+v", all)
	}
}

func noCheck(map[int64]int, *time.Time) error { return nil }

func TestUpsertUserKeepsHCAAddress(t *testing.T) {
	s := newStore(t)
	home := swag.Address{FirstName: "O", LastName: "D", Line1: "15 Falls Rd", City: "Shelburne", State: "VT", PostalCode: "05482", Country: "US", Phone: "+18025550199"}
	u, err := s.UpsertUser(ctx, auth.Identity{ID: "ident!1", Email: "a@x.com", Address: &home})
	if err != nil {
		t.Fatal(err)
	}
	if u.Address == nil || *u.Address != home {
		t.Fatalf("address = %+v", u.Address)
	}
	// A later login without the address scope must not wipe it.
	u, _ = s.UpsertUser(ctx, auth.Identity{ID: "ident!1", Email: "a@x.com"})
	if u.Address == nil || u.Address.Line1 != "15 Falls Rd" {
		t.Fatalf("address lost: %+v", u.Address)
	}
	tok, _ := s.CreateSession(ctx, u.ID, time.Hour)
	bySession, _ := s.UserBySession(ctx, tok)
	if bySession.Address == nil || bySession.Address.City != "Shelburne" {
		t.Fatalf("session user address = %+v", bySession.Address)
	}
}
