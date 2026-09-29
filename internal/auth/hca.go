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

	"github.com/hackclub/zach-mail-room/internal/swag"
)

// Identity is the subset of the Hack Club Auth identity this app uses.
type Identity struct {
	ID                 string
	Email              string
	Name               string
	SlackID            string
	VerificationStatus string
	Phone              string        // needs the "phone" scope
	Address            *swag.Address // primary address; needs the "address" scope
}

// Provider is what the HTTP layer needs from an identity provider.
type Provider interface {
	// redirectURL must be the same in both calls and registered with the provider.
	AuthCodeURL(state, redirectURL string) string
	Exchange(ctx context.Context, code, redirectURL string) (*Identity, error)
}

type HCAConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
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
			Scopes:       strings.Fields(c.Scopes),
			Endpoint: oauth2.Endpoint{
				AuthURL:   c.BaseURL + "/oauth/authorize",
				TokenURL:  c.BaseURL + "/oauth/token",
				AuthStyle: oauth2.AuthStyleInParams,
			},
		},
	}
}

func (h *HCA) AuthCodeURL(state, redirectURL string) string {
	return h.oauth.AuthCodeURL(state, oauth2.SetAuthURLParam("redirect_uri", redirectURL))
}

func (h *HCA) Exchange(ctx context.Context, code, redirectURL string) (*Identity, error) {
	tok, err := h.oauth.Exchange(ctx, code, oauth2.SetAuthURLParam("redirect_uri", redirectURL))
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
			PhoneNumber        string `json:"phone_number"`
			Addresses          []struct {
				FirstName   string `json:"first_name"`
				LastName    string `json:"last_name"`
				Line1       string `json:"line_1"`
				Line2       string `json:"line_2"`
				City        string `json:"city"`
				State       string `json:"state"`
				PostalCode  string `json:"postal_code"`
				Country     string `json:"country"`
				PhoneNumber string `json:"phone_number"`
				Primary     bool   `json:"primary"`
			} `json:"addresses"`
		} `json:"identity"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("hca me: %w", err)
	}
	i := body.Identity
	if i.ID == "" || i.PrimaryEmail == "" {
		return nil, fmt.Errorf("hca me: identity missing id or email (is the email scope granted?)")
	}
	id := &Identity{
		ID:                 i.ID,
		Email:              strings.ToLower(strings.TrimSpace(i.PrimaryEmail)),
		Name:               strings.TrimSpace(i.FirstName + " " + i.LastName),
		SlackID:            i.SlackID,
		VerificationStatus: i.VerificationStatus,
		Phone:              i.PhoneNumber,
	}
	// Prefer the primary address; fall back to the first one.
	pick := -1
	for n, a := range i.Addresses {
		if a.Primary || pick == -1 {
			pick = n
		}
		if a.Primary {
			break
		}
	}
	if pick >= 0 {
		a := i.Addresses[pick]
		phone := a.PhoneNumber
		if phone == "" {
			phone = i.PhoneNumber
		}
		id.Address = &swag.Address{
			FirstName: a.FirstName, LastName: a.LastName, Line1: a.Line1, Line2: a.Line2, City: a.City,
			State: a.State, PostalCode: a.PostalCode, Country: strings.ToUpper(a.Country), Phone: phone,
		}
	}
	return id, nil
}
