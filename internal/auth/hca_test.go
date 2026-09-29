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
		w.Write([]byte(`{"identity":{"id":"ident!abc","first_name":"Orpheus","last_name":"Dino","primary_email":"Orpheus@HackClub.com","slack_id":"U1","verification_status":"verified","ysws_eligible":true},"scopes":["email"]}`))
	})
	return httptest.NewServer(mux)
}

func TestAuthCodeURL(t *testing.T) {
	p := NewHCA(HCAConfig{BaseURL: "https://auth.example", ClientID: "cid", ClientSecret: "s", RedirectURL: "http://app/auth/callback", Scopes: "openid email"})
	u, err := url.Parse(p.AuthCodeURL("state123"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "auth.example" || u.Path != "/oauth/authorize" {
		t.Errorf("url = %s", u)
	}
	if q.Get("client_id") != "cid" || q.Get("state") != "state123" || q.Get("scope") != "openid email" || q.Get("response_type") != "code" || q.Get("redirect_uri") != "http://app/auth/callback" {
		t.Errorf("query = %v", q)
	}
}

func TestExchangeAndFetchIdentity(t *testing.T) {
	srv := fakeHCA(t)
	defer srv.Close()
	p := NewHCA(HCAConfig{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "s", RedirectURL: "http://app/auth/callback", Scopes: "openid email"})

	id, err := p.Exchange(context.Background(), "good-code")
	if err != nil {
		t.Fatal(err)
	}
	if id.ID != "ident!abc" || id.Email != "orpheus@hackclub.com" || id.Name != "Orpheus Dino" || id.SlackID != "U1" || id.VerificationStatus != "verified" {
		t.Fatalf("identity = %+v", id)
	}

	if _, err := p.Exchange(context.Background(), "bad-code"); err == nil || !strings.Contains(err.Error(), "exchange") {
		t.Fatalf("bad code err = %v", err)
	}
}
