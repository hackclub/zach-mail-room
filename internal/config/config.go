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
	Port    string
	BaseURL string // public origin, e.g. https://swag.hackclub.com
	// Other origins the app is reachable at (e.g. http://porygon:5173 over
	// Tailscale). Each one's /auth/callback must be registered with Hack Club Auth.
	ExtraBaseURLs []string
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
		HCAScopes:               get("HCA_SCOPES", "openid email name slack_id verification_status address phone"),
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

	for _, u := range strings.Split(getenv("EXTRA_BASE_URLS"), ",") {
		u = strings.TrimRight(strings.TrimSpace(u), "/")
		if u == "" {
			continue
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return nil, fmt.Errorf("EXTRA_BASE_URLS: %q must be an absolute http(s) origin", u)
		}
		c.ExtraBaseURLs = append(c.ExtraBaseURLs, u)
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

// Origins is BASE_URL followed by EXTRA_BASE_URLS.
func (c *Config) Origins() []string { return append([]string{c.BaseURL}, c.ExtraBaseURLs...) }

func (c *Config) IsAllowedOrigin(origin string) bool {
	for _, o := range c.Origins() {
		if o == origin {
			return true
		}
	}
	return false
}

// LookupOrigin returns the configured origin whose host is host, if any.
func (c *Config) LookupOrigin(host string) (string, bool) {
	for _, o := range c.Origins() {
		if strings.EqualFold(strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://"), strings.TrimSpace(host)) {
			return o, true
		}
	}
	return "", false
}

// OriginForHost picks the configured origin matching a request's host, so
// OAuth round-trips and cookies stay on the origin the user is actually
// using. Unknown hosts get BASE_URL.
func (c *Config) OriginForHost(host string) string {
	if o, ok := c.LookupOrigin(host); ok {
		return o
	}
	return c.BaseURL
}

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
