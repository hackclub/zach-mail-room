// Package theseus is a client for the mail.hackclub.com ("Theseus") warehouse
// API: SKU inventory and warehouse orders fulfilled by the Hack Club warehouse.
// Source of truth for the API: github.com/hackclub/theseus
// (app/controllers/api/v1/warehouse_*_controller.rb).
package theseus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: baseURL, apiKey: apiKey, http: hc}
}

type SKU struct {
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Enabled     bool   `json:"enabled"`
	InStock     int    `json:"in_stock"`
	Inbound     *int   `json:"inbound"`
	UnitCost    string `json:"unit_cost"`
}

type Address struct {
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Line1      string `json:"line_1"`
	Line2      string `json:"line_2,omitempty"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
	Phone      string `json:"phone_number,omitempty"`
}

type Content struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

type OrderInput struct {
	RecipientEmail string
	Title          string
	IdempotencyKey string
	Tags           []string
	Metadata       map[string]any
	Address        Address
	Contents       []Content
}

type Order struct {
	ID             string   `json:"id"`
	Status         string   `json:"status"`
	Tags           []string `json:"tags"`
	TrackingNumber string   `json:"tracking_number"`
	Carrier        string   `json:"carrier"`
	Service        string   `json:"service"`
	PostageCost    any      `json:"postage_cost"`
}

// ListSKUs returns enabled, in-inventory SKUs.
func (c *Client) ListSKUs(ctx context.Context) ([]SKU, error) {
	var out struct {
		SKUs []SKU `json:"skus"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/warehouse/skus", nil, &out)
	return out.SKUs, err
}

// CreateOrder creates and dispatches a warehouse order. Theseus replays the
// existing order for a repeated IdempotencyKey, so retries are safe.
func (c *Client) CreateOrder(ctx context.Context, in OrderInput) (*Order, error) {
	body := map[string]any{
		"warehouse_order": map[string]any{
			"recipient_email":   in.RecipientEmail,
			"user_facing_title": in.Title,
			"idempotency_key":   in.IdempotencyKey,
			"tags":              in.Tags,
			"metadata":          in.Metadata,
		},
		"address":  in.Address,
		"contents": in.Contents,
	}
	var o Order
	if err := c.do(ctx, http.MethodPost, "/api/v1/warehouse_orders", body, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

func (c *Client) GetOrder(ctx context.Context, id string) (*Order, error) {
	var o Order
	if err := c.do(ctx, http.MethodGet, "/api/v1/warehouse_orders/"+url.PathEscape(id), nil, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("theseus %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("theseus %s %s: HTTP %d: %s", method, path, resp.StatusCode, truncate(string(data), 500))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
