package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func fakeHCA(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("redirect_uri") != "http://porygon:5173/auth/callback" {
			http.Error(w, `{"error":"invalid_grant","error_description":"redirect_uri mismatch"}`, http.StatusBadRequest)
			return
		}
		if r.Form.Get("code") != "good-code" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"at","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"identity":{"id":"ident!abc","first_name":"Orpheus","last_name":"Dino","primary_email":"Orpheus@HackClub.com","slack_id":"U1","verification_status":"verified","ysws_eligible":true,"phone_number":"+18025550100",
			"addresses":[
				{"id":"addr!old","first_name":"Old","last_name":"Place","line_1":"1 Old St","city":"Oldtown","state":"CA","postal_code":"90001","country":"US","primary":false},
				{"id":"addr!new","first_name":"Orpheus","last_name":"Dino","line_1":"15 Falls Rd","line_2":"Apt 2","city":"Shelburne","state":"VT","postal_code":"05482","country":"US","phone_number":"+18025550199","primary":true}
			]},"scopes":["email","address","phone"]}`))
	})
	return httptest.NewServer(mux)
}

func TestAuthCodeURL(t *testing.T) {
	p := NewHCA(HCAConfig{BaseURL: "https://auth.example", ClientID: "cid", ClientSecret: "s", Scopes: "openid email"})
	u, err := url.Parse(p.AuthCodeURL("state123", "http://porygon:5173/auth/callback"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "auth.example" || u.Path != "/oauth/authorize" {
		t.Errorf("url = %s", u)
	}
	if q.Get("client_id") != "cid" || q.Get("state") != "state123" || q.Get("scope") != "openid email" || q.Get("response_type") != "code" || q.Get("redirect_uri") != "http://porygon:5173/auth/callback" {
		t.Errorf("query = %v", q)
	}
}

func TestExchangeAndFetchIdentity(t *testing.T) {
	srv := fakeHCA(t)
	defer srv.Close()
	p := NewHCA(HCAConfig{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "s", Scopes: "openid email"})

	id, err := p.Exchange(context.Background(), "good-code", "http://porygon:5173/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	if id.ID != "ident!abc" || id.Email != "orpheus@hackclub.com" || id.Name != "Orpheus Dino" || id.SlackID != "U1" || id.VerificationStatus != "verified" {
		t.Fatalf("identity = %+v", id)
	}
	if id.Phone != "+18025550100" {
		t.Errorf("phone = %q", id.Phone)
	}
	a := id.Address
	if a == nil || a.Line1 != "15 Falls Rd" || a.Line2 != "Apt 2" || a.Country != "US" || a.Phone != "+18025550199" {
		t.Fatalf("should pick the primary address: %+v", a)
	}
	if _, err := p.Exchange(context.Background(), "good-code", "http://localhost:5173/auth/callback"); err == nil {
		t.Fatal("exchange must send the same redirect_uri the flow started with")
	}

	if _, err := p.Exchange(context.Background(), "bad-code", "http://porygon:5173/auth/callback"); err == nil || !strings.Contains(err.Error(), "exchange") {
		t.Fatalf("bad code err = %v", err)
	}
}

func TestIdentityWithoutAddressScope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"at","token_type":"Bearer"}`))
	})
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"identity":{"id":"ident!x","primary_email":"x@example.com"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	id, err := NewHCA(HCAConfig{BaseURL: srv.URL, ClientID: "c", ClientSecret: "s"}).Exchange(context.Background(), "c", "http://app/auth/callback")
	if err != nil || id.Address != nil || id.Phone != "" {
		t.Fatalf("id = %+v, err = %v", id, err)
	}
}
