// Package authors decides whether a signed-in person is a YSWS author: someone
// whose email appears in the "YSWS Authors" table of the Unified YSWS Projects
// DB (Airtable base app3A5kJwYqxMLOgh, field "Hack Club Auth Email").
package authors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Directory interface {
	IsAuthor(ctx context.Context, email string) (bool, error)
}

// Static is an in-memory Directory for tests and local development.
type Static map[string]bool

func (s Static) IsAuthor(_ context.Context, email string) (bool, error) {
	return s[normalize(email)], nil
}

type AirtableConfig struct {
	BaseURL    string // default https://api.airtable.com
	APIKey     string
	BaseID     string
	Table      string
	EmailField string
	CacheTTL   time.Duration
	HTTP       *http.Client
}

type Airtable struct {
	cfg   AirtableConfig
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	ok  bool
	exp time.Time
}

func NewAirtable(cfg AirtableConfig) *Airtable {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.airtable.com"
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 10 * time.Minute
	}
	return &Airtable{cfg: cfg, cache: map[string]cached{}}
}

func (a *Airtable) IsAuthor(ctx context.Context, email string) (bool, error) {
	email = normalize(email)
	if email == "" {
		return false, nil
	}
	a.mu.Lock()
	if c, ok := a.cache[email]; ok && time.Now().Before(c.exp) {
		a.mu.Unlock()
		return c.ok, nil
	}
	a.mu.Unlock()

	q := url.Values{}
	q.Set("filterByFormula", formula(a.cfg.EmailField, email))
	q.Set("maxRecords", "1")
	q.Add("fields[]", a.cfg.EmailField)
	u := fmt.Sprintf("%s/v0/%s/%s?%s", a.cfg.BaseURL, url.PathEscape(a.cfg.BaseID), url.PathEscape(a.cfg.Table), q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	resp, err := a.cfg.HTTP.Do(req)
	if err != nil {
		return false, fmt.Errorf("airtable authors lookup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("airtable authors lookup: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Records []struct {
			ID string `json:"id"`
		} `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, err
	}
	ok := len(body.Records) > 0
	a.mu.Lock()
	a.cache[email] = cached{ok: ok, exp: time.Now().Add(a.cfg.CacheTTL)}
	a.mu.Unlock()
	return ok, nil
}

func formula(field, email string) string {
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(email)
	return fmt.Sprintf(`LOWER(TRIM({%s}))="%s"`, field, esc)
}

func normalize(e string) string { return strings.ToLower(strings.TrimSpace(e)) }
