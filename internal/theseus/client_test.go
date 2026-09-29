package theseus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListSKUs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/warehouse/skus" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("auth header = %q", got)
		}
		if r.URL.Query().Get("all") != "" {
			t.Errorf("unexpected all param")
		}
		w.Write([]byte(`{"skus":[{"sku":"Sti/A","name":"A","category":"sticker","enabled":true,"in_stock":10,"inbound":null,"unit_cost":"0.25"}]}`))
	}))
	defer srv.Close()

	skus, err := New(srv.URL, "k", nil).ListSKUs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(skus) != 1 || skus[0].SKU != "Sti/A" || skus[0].InStock != 10 || skus[0].UnitCost != "0.25" {
		t.Fatalf("skus = %+v", skus)
	}
}

func TestCreateOrder(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/warehouse_orders" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"pkg!abc","status":"dispatched","tags":["zach-mail-room"]}`))
	}))
	defer srv.Close()

	o, err := New(srv.URL, "k", nil).CreateOrder(context.Background(), OrderInput{
		RecipientEmail: "orpheus@hackclub.com",
		Title:          "Hack Club swag",
		IdempotencyKey: "zmr-request-7",
		Tags:           []string{"zach-mail-room"},
		Address:        Address{FirstName: "O", LastName: "D", Line1: "1 St", City: "C", State: "VT", PostalCode: "05401", Country: "US"},
		Contents:       []Content{{SKU: "Sti/A", Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.ID != "pkg!abc" || o.Status != "dispatched" {
		t.Fatalf("order = %+v", o)
	}
	wo := body["warehouse_order"].(map[string]any)
	if wo["recipient_email"] != "orpheus@hackclub.com" || wo["idempotency_key"] != "zmr-request-7" || wo["user_facing_title"] != "Hack Club swag" {
		t.Errorf("warehouse_order = %v", wo)
	}
	if body["address"].(map[string]any)["line_1"] != "1 St" {
		t.Errorf("address = %v", body["address"])
	}
	c := body["contents"].([]any)[0].(map[string]any)
	if c["sku"] != "Sti/A" || c["quantity"].(float64) != 2 {
		t.Errorf("contents = %v", body["contents"])
	}
}

func TestErrorsIncludeAPIMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"error":"SKU not found: Nope"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL, "k", nil).CreateOrder(context.Background(), OrderInput{})
	if err == nil || !strings.Contains(err.Error(), "SKU not found") || !strings.Contains(err.Error(), "422") {
		t.Fatalf("err = %v", err)
	}
}

func TestGetOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/warehouse_orders/pkg!abc" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(`{"id":"pkg!abc","status":"mailed","tracking_number":"9400","carrier":"USPS"}`))
	}))
	defer srv.Close()
	o, err := New(srv.URL, "k", nil).GetOrder(context.Background(), "pkg!abc")
	if err != nil {
		t.Fatal(err)
	}
	if o.TrackingNumber != "9400" || o.Carrier != "USPS" || o.Status != "mailed" {
		t.Fatalf("order = %+v", o)
	}
}
