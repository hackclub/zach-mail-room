// Package config loads all runtime configuration from environment variables
// (12-factor). Nothing here reads files: in development the Makefile sources the
// .env file in the main worktree before starting the server.
package config

import (
	"errors"
	"fmt"
	"strings"
)

type Config struct {
	Port          string
	BaseURL       string // public origin, e.g. https://swag.hackclub.com
	DatabaseURL   string
	SessionSecret string

	// Hack Club Auth (OAuth2 / OIDC)
	HCABaseURL      string
	HCAClientID     string
	HCAClientSecret string
	HCAScopes       string

	// mail.hackclub.com (Theseus) warehouse API
	TheseusBaseURL  string
	TheseusAPIKey   string
	TheseusOrderTag string

	// Unified YSWS Projects DB (Airtable) — source of YSWS authors
	AirtableAPIKey          string
	AirtableAuthorsBaseID   string
	AirtableAuthorsTable    string
	AirtableAuthorsEmailFld string

	// HCB donation page international recipients pay shipping through
	HCBShippingPaymentURL string

	AdminEmails []string

	// Directory holding the built PWA (web/build). Empty disables static serving.
	StaticDir string
}

// Load builds a Config from getenv (usually os.Getenv).
func Load(getenv func(string) string) (*Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	c := &Config{
		Port:                    get("PORT", "8080"),
		BaseURL:                 strings.TrimRight(get("BASE_URL", ""), "/"),
		DatabaseURL:             get("DATABASE_URL", ""),
		SessionSecret:           get("SESSION_SECRET", ""),
		HCABaseURL:              strings.TrimRight(get("HCA_BASE_URL", "https://auth.hackclub.com"), "/"),
		HCAClientID:             get("HCA_CLIENT_ID", ""),
		HCAClientSecret:         get("HCA_CLIENT_SECRET", ""),
		HCAScopes:               get("HCA_SCOPES", "openid email name slack_id verification_status"),
		TheseusBaseURL:          strings.TrimRight(get("THESEUS_BASE_URL", "https://mail.hackclub.com"), "/"),
		TheseusAPIKey:           get("THESEUS_API_KEY", ""),
		TheseusOrderTag:         get("THESEUS_ORDER_TAG", "zach-mail-room"),
		AirtableAPIKey:          get("AIRTABLE_API_KEY", ""),
		AirtableAuthorsBaseID:   get("AIRTABLE_AUTHORS_BASE_ID", "app3A5kJwYqxMLOgh"),
		AirtableAuthorsTable:    get("AIRTABLE_AUTHORS_TABLE", "YSWS Authors"),
		AirtableAuthorsEmailFld: get("AIRTABLE_AUTHORS_EMAIL_FIELD", "Hack Club Auth Email"),
		HCBShippingPaymentURL:   get("HCB_SHIPPING_PAYMENT_URL", ""),
		StaticDir:               get("STATIC_DIR", "web/build"),
	}
	for _, e := range strings.Split(getenv("ADMIN_EMAILS"), ",") {
		if e = normalizeEmail(e); e != "" {
			c.AdminEmails = append(c.AdminEmails, e)
		}
	}

	var missing []string
	for k, v := range map[string]string{
		"DATABASE_URL":      c.DatabaseURL,
		"BASE_URL":          c.BaseURL,
		"SESSION_SECRET":    c.SessionSecret,
		"HCA_CLIENT_ID":     c.HCAClientID,
		"HCA_CLIENT_SECRET": c.HCAClientSecret,
		"THESEUS_API_KEY":   c.TheseusAPIKey,
	} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	if len(c.SessionSecret) < 32 {
		return nil, errors.New("SESSION_SECRET must be at least 32 characters")
	}
	return c, nil
}

func (c *Config) OAuthRedirectURL() string { return c.BaseURL + "/auth/callback" }

func (c *Config) SecureCookies() bool { return strings.HasPrefix(c.BaseURL, "https://") }

func (c *Config) IsAdminEmail(email string) bool {
	email = normalizeEmail(email)
	if email == "" {
		return false
	}
	for _, a := range c.AdminEmails {
		if a == email {
			return true
		}
	}
	return false
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }
