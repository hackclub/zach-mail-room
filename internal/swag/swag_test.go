package swag

import (
	"errors"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func settings() Settings {
	return Settings{
		RequestsOpen:                  true,
		MaxItemsPerRequest:            5,
		RequestCooldownDays:           30,
		InternationalShippingFeeCents: 1500,
	}
}

func catalog() map[int64]Item {
	return map[int64]Item{
		1: {ID: 1, SKU: "Sti/Pck/1", Name: "Sticker pack", Visible: true, MaxPerRequest: 3},
		2: {ID: 2, SKU: "Swa/Sox/1", Name: "Socks", Visible: true, MaxPerRequest: 1, MaxPerUser: intp(1)},
		3: {ID: 3, SKU: "Swa/Hid/1", Name: "Hidden hoodie", Visible: false, MaxPerRequest: 1},
	}
}

func intp(i int) *int { return &i }

func TestValidateOK(t *testing.T) {
	err := Validate(ValidateInput{
		Settings: settings(),
		Catalog:  catalog(),
		Lines:    []Line{{ItemID: 1, Quantity: 3}, {ItemID: 2, Quantity: 1}},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateErrors(t *testing.T) {
	last := now.Add(-10 * 24 * time.Hour)
	oldEnough := now.Add(-31 * 24 * time.Hour)
	closed := settings()
	closed.RequestsOpen = false

	cases := []struct {
		name string
		in   ValidateInput
		want error
	}{
		{"closed", ValidateInput{Settings: closed, Catalog: catalog(), Lines: []Line{{1, 1}}}, ErrRequestsClosed},
		{"empty", ValidateInput{Settings: settings(), Catalog: catalog()}, ErrNoItems},
		{"zero qty", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{1, 0}}}, ErrBadQuantity},
		{"unknown item", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{99, 1}}}, ErrUnknownItem},
		{"hidden item", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{3, 1}}}, ErrUnknownItem},
		{"duplicate line", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{1, 1}, {1, 1}}}, ErrDuplicateItem},
		{"over per-request", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{1, 4}}}, ErrItemLimit},
		{"over per-user lifetime", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{2, 1}}, PriorQuantities: map[int64]int{2: 1}}, ErrItemLimit},
		{"over total", func() ValidateInput {
			s := settings()
			s.MaxItemsPerRequest = 2
			return ValidateInput{Settings: s, Catalog: catalog(), Lines: []Line{{1, 2}, {2, 1}}}
		}(), ErrTooManyItems},
		{"cooldown", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{1, 1}}, LastRequestAt: &last, Now: now}, ErrCooldown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.in.Now.IsZero() {
				tc.in.Now = now
			}
			err := Validate(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("cooldown elapsed", func(t *testing.T) {
		err := Validate(ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{1, 1}}, LastRequestAt: &oldEnough, Now: now})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestIsInternational(t *testing.T) {
	for country, want := range map[string]bool{"US": false, "us": false, " US ": false, "CA": true, "IN": true} {
		if got := IsInternational(country); got != want {
			t.Errorf("IsInternational(%q) = %v, want %v", country, got, want)
		}
	}
}

func TestInitialStatus(t *testing.T) {
	st, fee := InitialStatus(settings(), "US")
	if st != StatusPending || fee != 0 {
		t.Errorf("US: got %s/%d", st, fee)
	}
	st, fee = InitialStatus(settings(), "DE")
	if st != StatusAwaitingPayment || fee != 1500 {
		t.Errorf("DE: got %s/%d", st, fee)
	}
	free := settings()
	free.InternationalShippingFeeCents = 0
	if st, _ := InitialStatus(free, "DE"); st != StatusPending {
		t.Errorf("free international shipping should skip payment, got %s", st)
	}
}

func TestValidAddress(t *testing.T) {
	good := Address{FirstName: "Orpheus", LastName: "Dino", Line1: "8605 Santa Monica Blvd", City: "West Hollywood", State: "CA", PostalCode: "90069", Country: "US"}
	if err := good.Validate(); err != nil {
		t.Fatalf("good address: %v", err)
	}
	bad := good
	bad.Line1 = " "
	if err := bad.Validate(); err == nil {
		t.Error("missing line1 should fail")
	}
	bad = good
	bad.Country = "USA"
	if err := bad.Validate(); err == nil {
		t.Error("country must be ISO alpha-2")
	}
	intl := good
	intl.Country = "DE"
	if err := intl.Validate(); err == nil {
		t.Error("international address without phone should fail (customs)")
	}
	intl.Phone = "+49 30 1234567"
	if err := intl.Validate(); err != nil {
		t.Errorf("international with phone: %v", err)
	}
}

func TestCanTransition(t *testing.T) {
	ok := [][2]Status{
		{StatusAwaitingPayment, StatusPending},
		{StatusAwaitingPayment, StatusCancelled},
		{StatusPending, StatusDispatched},
		{StatusPending, StatusRejected},
		{StatusPending, StatusCancelled},
	}
	for _, p := range ok {
		if !CanTransition(p[0], p[1]) {
			t.Errorf("%s -> %s should be allowed", p[0], p[1])
		}
	}
	bad := [][2]Status{
		{StatusAwaitingPayment, StatusDispatched},
		{StatusDispatched, StatusCancelled},
		{StatusRejected, StatusPending},
	}
	for _, p := range bad {
		if CanTransition(p[0], p[1]) {
			t.Errorf("%s -> %s should be refused", p[0], p[1])
		}
	}
}
