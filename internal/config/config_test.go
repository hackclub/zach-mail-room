package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":      "postgres://localhost/x",
		"BASE_URL":          "http://localhost:8080",
		"SESSION_SECRET":    "0123456789abcdef0123456789abcdef",
		"HCA_CLIENT_ID":     "id",
		"HCA_CLIENT_SECRET": "secret",
		"THESEUS_API_KEY":   "th_api_test",
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(validEnv()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Port != "8080" {
		t.Errorf("Port = %q, want 8080", c.Port)
	}
	if c.HCABaseURL != "https://auth.hackclub.com" {
		t.Errorf("HCABaseURL = %q", c.HCABaseURL)
	}
	if c.TheseusBaseURL != "https://mail.hackclub.com" {
		t.Errorf("TheseusBaseURL = %q", c.TheseusBaseURL)
	}
	if c.StaticDir != "web/build" {
		t.Errorf("StaticDir = %q", c.StaticDir)
	}
	if c.AirtableAuthorsBaseID != "app3A5kJwYqxMLOgh" {
		t.Errorf("AirtableAuthorsBaseID = %q", c.AirtableAuthorsBaseID)
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	for _, key := range []string{"DATABASE_URL", "BASE_URL", "SESSION_SECRET", "HCA_CLIENT_ID", "HCA_CLIENT_SECRET", "THESEUS_API_KEY"} {
		e := validEnv()
		delete(e, key)
		_, err := Load(env(e))
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("missing %s: err = %v, want mention of key", key, err)
		}
	}
}

func TestLoadRejectsShortSessionSecret(t *testing.T) {
	e := validEnv()
	e["SESSION_SECRET"] = "short"
	if _, err := Load(env(e)); err == nil {
		t.Fatal("expected error for short SESSION_SECRET")
	}
}

func TestAdminEmailsNormalized(t *testing.T) {
	e := validEnv()
	e["ADMIN_EMAILS"] = " Zach@HackClub.com , other@example.com,,"
	c, err := Load(env(e))
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsAdminEmail("zach@hackclub.com") || !c.IsAdminEmail("OTHER@example.com ") {
		t.Errorf("admin emails not matched: %v", c.AdminEmails)
	}
	if c.IsAdminEmail("nobody@example.com") || c.IsAdminEmail("") {
		t.Error("unexpected admin match")
	}
}

func TestBaseURLTrailingSlashTrimmed(t *testing.T) {
	e := validEnv()
	e["BASE_URL"] = "https://swag.hackclub.com/"
	c, err := Load(env(e))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Origins(); len(got) != 1 || got[0] != "https://swag.hackclub.com" {
		t.Errorf("Origins = %v", got)
	}
}

func TestDefaultScopesIncludeAddressAndPhone(t *testing.T) {
	c, _ := Load(env(validEnv()))
	for _, s := range []string{"openid", "email", "name", "slack_id", "verification_status", "address", "phone"} {
		if !strings.Contains(" "+c.HCAScopes+" ", " "+s+" ") {
			t.Errorf("default scopes %q missing %q", c.HCAScopes, s)
		}
	}
}

func TestExtraBaseURLs(t *testing.T) {
	e := validEnv()
	e["BASE_URL"] = "http://localhost:5173"
	e["EXTRA_BASE_URLS"] = " http://porygon:5173/ , ,"
	c, err := Load(env(e))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Origins(); len(got) != 2 || got[0] != "http://localhost:5173" || got[1] != "http://porygon:5173" {
		t.Fatalf("Origins = %v", got)
	}
	if o := c.OriginForHost("porygon:5173"); o != "http://porygon:5173" {
		t.Errorf("OriginForHost(porygon) = %q", o)
	}
	if o := c.OriginForHost("evil.example"); o != "http://localhost:5173" {
		t.Errorf("unknown host should fall back to BASE_URL, got %q", o)
	}
	if !c.IsAllowedOrigin("http://porygon:5173") || c.IsAllowedOrigin("http://evil.example") {
		t.Error("IsAllowedOrigin wrong")
	}

	e["EXTRA_BASE_URLS"] = "porygon:5173"
	if _, err := Load(env(e)); err == nil {
		t.Error("EXTRA_BASE_URLS entries must be absolute http(s) origins")
	}
}
