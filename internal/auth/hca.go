// Package auth implements sign-in with Hack Club Auth (auth.hackclub.com), an
// OAuth2/OIDC provider. Docs: https://auth.hackclub.com/docs/oauth-guide
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// Identity is the subset of the Hack Club Auth identity this app uses.
type Identity struct {
	ID                 string
	Email              string
	Name               string
	SlackID            string
	VerificationStatus string
}

// Provider is what the HTTP layer needs from an identity provider.
type Provider interface {
	AuthCodeURL(state string) string
	Exchange(ctx context.Context, code string) (*Identity, error)
}

type HCAConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string
}

type HCA struct {
	baseURL string
	oauth   *oauth2.Config
}

func NewHCA(c HCAConfig) *HCA {
	return &HCA{
		baseURL: strings.TrimRight(c.BaseURL, "/"),
		oauth: &oauth2.Config{
			ClientID:     c.ClientID,
			ClientSecret: c.ClientSecret,
			RedirectURL:  c.RedirectURL,
			Scopes:       strings.Fields(c.Scopes),
			Endpoint: oauth2.Endpoint{
				AuthURL:   c.BaseURL + "/oauth/authorize",
				TokenURL:  c.BaseURL + "/oauth/token",
				AuthStyle: oauth2.AuthStyleInParams,
			},
		},
	}
}

func (h *HCA) AuthCodeURL(state string) string { return h.oauth.AuthCodeURL(state) }

func (h *HCA) Exchange(ctx context.Context, code string) (*Identity, error) {
	tok, err := h.oauth.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("hca exchange: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/api/v1/me", nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.oauth.Client(ctx, tok).Do(req)
	if err != nil {
		return nil, fmt.Errorf("hca me: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hca me: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Identity struct {
			ID                 string `json:"id"`
			FirstName          string `json:"first_name"`
			LastName           string `json:"last_name"`
			PrimaryEmail       string `json:"primary_email"`
			SlackID            string `json:"slack_id"`
			VerificationStatus string `json:"verification_status"`
		} `json:"identity"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("hca me: %w", err)
	}
	i := body.Identity
	if i.ID == "" || i.PrimaryEmail == "" {
		return nil, fmt.Errorf("hca me: identity missing id or email (is the email scope granted?)")
	}
	return &Identity{
		ID:                 i.ID,
		Email:              strings.ToLower(strings.TrimSpace(i.PrimaryEmail)),
		Name:               strings.TrimSpace(i.FirstName + " " + i.LastName),
		SlackID:            i.SlackID,
		VerificationStatus: i.VerificationStatus,
	}, nil
}
