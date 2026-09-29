package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hackclub/zach-mail-room/internal/auth"
	"github.com/hackclub/zach-mail-room/internal/authors"
	"github.com/hackclub/zach-mail-room/internal/config"
	"github.com/hackclub/zach-mail-room/internal/db/dbtest"
	"github.com/hackclub/zach-mail-room/internal/store"
	"github.com/hackclub/zach-mail-room/internal/swag"
	"github.com/hackclub/zach-mail-room/internal/theseus"
)

// ---- fakes ----

type fakeProvider struct{ ids map[string]auth.Identity }

func (f fakeProvider) AuthCodeURL(state string) string {
	return "https://auth.example/oauth/authorize?state=" + url.QueryEscape(state)
}

func (f fakeProvider) Exchange(_ context.Context, code string) (*auth.Identity, error) {
	id, ok := f.ids[code]
	if !ok {
		return nil, fmt.Errorf("bad code")
	}
	return &id, nil
}

type fakeWarehouse struct {
	mu     sync.Mutex
	orders []theseus.OrderInput
	fail   error
}

func (f *fakeWarehouse) ListSKUs(context.Context) ([]theseus.SKU, error) {
	return []theseus.SKU{{SKU: "Sti/A", Name: "Sticker A", InStock: 100, Enabled: true}}, nil
}

func (f *fakeWarehouse) CreateOrder(_ context.Context, in theseus.OrderInput) (*theseus.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	f.orders = append(f.orders, in)
	return &theseus.Order{ID: fmt.Sprintf("pkg!%d", len(f.orders)), Status: "dispatched"}, nil
}

func (f *fakeWarehouse) GetOrder(_ context.Context, id string) (*theseus.Order, error) {
	return &theseus.Order{ID: id, Status: "mailed", TrackingNumber: "9400", Carrier: "USPS"}, nil
}

// ---- harness ----

type harness struct {
	t     *testing.T
	srv   *httptest.Server
	store *store.Store
	wh    *fakeWarehouse
	cfg   *config.Config
}

const (
	adminEmail  = "zach@hackclub.com"
	authorEmail = "author@hackclub.com"
	userEmail   = "hacker@example.com"
)

func newHarness(t *testing.T) *harness {
	t.Helper()
	st := store.New(dbtest.New(t))
	cfg := &config.Config{
		BaseURL:               "http://app.test",
		AdminEmails:           []string{adminEmail},
		TheseusOrderTag:       "zach-mail-room",
		HCBShippingPaymentURL: "https://hcb.hackclub.com/donations/start/swag",
	}
	wh := &fakeWarehouse{}
	h := New(Deps{
		Config: cfg,
		Store:  st,
		Provider: fakeProvider{ids: map[string]auth.Identity{
			"code-user": {ID: "ident!user", Email: userEmail, Name: "Hacker"},
		}},
		Authors:   authors.Static{authorEmail: true},
		Warehouse: wh,
		Static: fstest.MapFS{
			"index.html":           {Data: []byte("<html>app shell</html>")},
			"manifest.webmanifest": {Data: []byte(`{"name":"x"}`)},
		},
		Now: time.Now,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &harness{t: t, srv: srv, store: st, wh: wh, cfg: cfg}
}

// client returns an http.Client logged in as email ("" = anonymous).
func (h *harness) client(email string) *http.Client {
	h.t.Helper()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if email == "" {
		return c
	}
	u, err := h.store.UpsertUser(context.Background(), auth.Identity{ID: "ident!" + email, Email: email, Name: email})
	if err != nil {
		h.t.Fatal(err)
	}
	tok, err := h.store.CreateSession(context.Background(), u.ID, time.Hour)
	if err != nil {
		h.t.Fatal(err)
	}
	c.Transport = cookieTransport{tok: tok}
	return c
}

type cookieTransport struct{ tok string }

func (c cookieTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: c.tok})
	return http.DefaultTransport.RoundTrip(r)
}

func (h *harness) do(c *http.Client, method, path string, body any, out any) int {
	h.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			h.t.Fatalf("%s %s: decode %q: %v", method, path, data, err)
		}
	}
	return resp.StatusCode
}

func (h *harness) mkItem(sku string, visible bool) swag.Item {
	h.t.Helper()
	it, err := h.store.CreateItem(context.Background(), swag.Item{SKU: sku, Name: sku, Visible: visible, MaxPerRequest: 3})
	if err != nil {
		h.t.Fatal(err)
	}
	return it
}

var usAddr = swag.Address{FirstName: "O", LastName: "D", Line1: "1 Main", City: "Burlington", State: "VT", PostalCode: "05401", Country: "US"}
var deAddr = swag.Address{FirstName: "O", LastName: "D", Line1: "Str 1", City: "Berlin", PostalCode: "10115", Country: "DE", Phone: "+49301234567"}

// ---- tests ----

func TestHealthz(t *testing.T) {
	h := newHarness(t)
	if code := h.do(h.client(""), "GET", "/healthz", nil, nil); code != 200 {
		t.Fatalf("healthz = %d", code)
	}
}

func TestSPAFallbackAndStatic(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/", "/admin", "/requests/12"} {
		resp, _ := http.Get(h.srv.URL + p)
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(b), "app shell") {
			t.Errorf("%s -> %d %q", p, resp.StatusCode, b)
		}
	}
	resp, _ := http.Get(h.srv.URL + "/manifest.webmanifest")
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("manifest -> %d", resp.StatusCode)
	}
	resp, _ = http.Get(h.srv.URL + "/api/nope")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("unknown api path should 404, got %d", resp.StatusCode)
	}
}

func TestLoginFlow(t *testing.T) {
	h := newHarness(t)
	jar := newJar()
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := c.Get(h.srv.URL + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	state := loc.Query().Get("state")
	if resp.StatusCode != http.StatusFound || state == "" {
		t.Fatalf("login -> %d %s", resp.StatusCode, loc)
	}

	// Wrong state is refused.
	resp, _ = c.Get(h.srv.URL + "/auth/callback?code=code-user&state=forged")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged state -> %d", resp.StatusCode)
	}

	resp, _ = c.Get(h.srv.URL + "/auth/callback?code=code-user&state=" + url.QueryEscape(state))
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("callback -> %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	var me meResponse
	h.do(c, "GET", "/api/me", nil, &me)
	if me.User == nil || me.User.Email != userEmail || me.Roles.Admin || me.Roles.Author {
		t.Fatalf("me = %+v", me)
	}

	req, _ := http.NewRequest("POST", h.srv.URL+"/auth/logout", nil)
	resp, _ = c.Do(req)
	resp.Body.Close()
	me = meResponse{}
	h.do(c, "GET", "/api/me", nil, &me)
	if me.User != nil {
		t.Fatalf("still logged in after logout: %+v", me)
	}
}

func TestRoles(t *testing.T) {
	h := newHarness(t)
	cases := map[string]roles{
		"":          {},
		userEmail:   {},
		authorEmail: {Author: true},
		adminEmail:  {Author: true, Admin: true},
	}
	for email, want := range cases {
		var me meResponse
		h.do(h.client(email), "GET", "/api/me", nil, &me)
		if me.Roles != want {
			t.Errorf("%q roles = %+v, want %+v", email, me.Roles, want)
		}
	}
}

func TestAccessControl(t *testing.T) {
	h := newHarness(t)
	anon, user, author, admin := h.client(""), h.client(userEmail), h.client(authorEmail), h.client(adminEmail)
	checks := []struct {
		c    *http.Client
		path string
		want int
	}{
		{anon, "/api/requests", 401},
		{user, "/api/requests", 200},
		{user, "/api/author/submissions", 403},
		{author, "/api/author/submissions", 200},
		{author, "/api/admin/requests", 403},
		{user, "/api/admin/settings", 403},
		{admin, "/api/admin/requests", 200},
		{admin, "/api/admin/settings", 200},
		{admin, "/api/admin/submissions", 200},
		{admin, "/api/admin/items", 200},
		{admin, "/api/admin/warehouse/skus", 200},
	}
	for _, c := range checks {
		if got := h.do(c.c, "GET", c.path, nil, nil); got != c.want {
			t.Errorf("GET %s = %d, want %d", c.path, got, c.want)
		}
	}
}

func TestMutationsRequireJSONAndSameOrigin(t *testing.T) {
	h := newHarness(t)
	c := h.client(userEmail)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/requests", strings.NewReader("a=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ := c.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form post -> %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", h.srv.URL+"/api/requests", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, _ = c.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin post -> %d", resp.StatusCode)
	}
}

func TestCatalogShowsOnlyVisibleItems(t *testing.T) {
	h := newHarness(t)
	h.mkItem("Sti/A", true)
	h.mkItem("Sti/Hidden", false)
	var out struct {
		Items    []swag.Item `json:"items"`
		Settings struct {
			RequestsOpen                  bool   `json:"requests_open"`
			InternationalShippingFeeCents int    `json:"international_shipping_fee_cents"`
			HCBPaymentURL                 string `json:"hcb_payment_url"`
		} `json:"settings"`
	}
	if code := h.do(h.client(""), "GET", "/api/catalog", nil, &out); code != 200 {
		t.Fatalf("catalog = %d", code)
	}
	if len(out.Items) != 1 || out.Items[0].SKU != "Sti/A" {
		t.Fatalf("items = %+v", out.Items)
	}
	if !out.Settings.RequestsOpen || out.Settings.InternationalShippingFeeCents != 1500 || out.Settings.HCBPaymentURL == "" {
		t.Fatalf("settings = %+v", out.Settings)
	}
}

func TestDomesticRequestFlow(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	user, admin := h.client(userEmail), h.client(adminEmail)

	var r store.Request
	code := h.do(user, "POST", "/api/requests", map[string]any{"address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 2}}, "note": "thanks!"}, &r)
	if code != 201 || r.Status != swag.StatusPending || r.ShippingFeeCents != 0 {
		t.Fatalf("create = %d %+v", code, r)
	}

	// Cooldown blocks an immediate second request.
	var errBody map[string]string
	if code := h.do(user, "POST", "/api/requests", map[string]any{"address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &errBody); code != 422 || !strings.Contains(errBody["error"], "recently") {
		t.Fatalf("second request = %d %v", code, errBody)
	}

	var mine struct{ Requests []store.Request }
	h.do(user, "GET", "/api/requests", nil, &mine)
	if len(mine.Requests) != 1 {
		t.Fatalf("mine = %+v", mine)
	}

	var dispatched store.Request
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, &dispatched); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	if dispatched.Status != swag.StatusDispatched || dispatched.TheseusOrderID != "pkg!1" {
		t.Fatalf("dispatched = %+v", dispatched)
	}
	o := h.wh.orders[0]
	if o.RecipientEmail != userEmail || o.IdempotencyKey != fmt.Sprintf("zach-mail-room-request-%d", r.ID) || o.Tags[0] != "zach-mail-room" ||
		o.Contents[0].SKU != "Sti/A" || o.Contents[0].Quantity != 2 || o.Address.Line1 != "1 Main" {
		t.Fatalf("theseus order = %+v", o)
	}

	var tracked store.Request
	h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/refresh", r.ID), map[string]any{}, &tracked)
	if tracked.TrackingNumber != "9400" {
		t.Fatalf("tracked = %+v", tracked)
	}
}

func TestDispatchFailureLeavesRequestPending(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	var r store.Request
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r)
	h.wh.fail = fmt.Errorf("theseus down")
	if code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 502 {
		t.Fatalf("dispatch = %d", code)
	}
	got, _ := h.store.GetRequest(context.Background(), r.ID)
	if got.Status != swag.StatusPending {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestInternationalRequestNeedsPayment(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	user, admin := h.client(userEmail), h.client(adminEmail)

	var r struct {
		store.Request
		PaymentURL string `json:"payment_url"`
	}
	code := h.do(user, "POST", "/api/requests", map[string]any{"address": deAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r)
	if code != 201 || r.Status != swag.StatusAwaitingPayment || r.ShippingFeeCents != 1500 {
		t.Fatalf("create = %d %+v", code, r)
	}
	if !strings.HasPrefix(r.PaymentURL, h.cfg.HCBShippingPaymentURL) || !strings.Contains(r.PaymentURL, "amount=1500") {
		t.Errorf("payment_url = %q", r.PaymentURL)
	}

	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 409 {
		t.Fatalf("dispatch unpaid = %d, want 409", code)
	}
	var paid store.Request
	h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/mark-paid", r.ID), map[string]any{}, &paid)
	if paid.Status != swag.StatusPending || paid.PaidAt == nil {
		t.Fatalf("paid = %+v", paid)
	}
}

func TestUserCanCancelOwnPendingRequestOnly(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	var r store.Request
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r)

	if code := h.do(h.client(authorEmail), "POST", fmt.Sprintf("/api/requests/%d/cancel", r.ID), map[string]any{}, nil); code != 404 {
		t.Fatalf("other user cancel = %d", code)
	}
	var got store.Request
	if code := h.do(h.client(userEmail), "POST", fmt.Sprintf("/api/requests/%d/cancel", r.ID), map[string]any{}, &got); code != 200 || got.Status != swag.StatusCancelled {
		t.Fatalf("cancel = %d %+v", code, got)
	}
}

func TestRequestValidation(t *testing.T) {
	h := newHarness(t)
	hidden := h.mkItem("Sti/Hidden", false)
	it := h.mkItem("Sti/A", true)
	user := h.client(userEmail)
	bad := []map[string]any{
		{"address": usAddr, "lines": []swag.Line{}},
		{"address": usAddr, "lines": []swag.Line{{ItemID: hidden.ID, Quantity: 1}}},
		{"address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 99}}},
		{"address": swag.Address{Country: "US"}, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}},
	}
	for i, b := range bad {
		if code := h.do(user, "POST", "/api/requests", b, nil); code != 422 {
			t.Errorf("case %d = %d, want 422", i, code)
		}
	}
}

func TestAdminManagesItemsAndSettings(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)

	var it swag.Item
	if code := h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Sti/A", Name: "Sticker", MaxPerRequest: 2, Visible: true}, &it); code != 201 || it.ID == 0 {
		t.Fatalf("create item = %d %+v", code, it)
	}
	if code := h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Sti/A", Name: "Dup", MaxPerRequest: 1}, nil); code != 409 {
		t.Fatalf("dup item = %d", code)
	}
	it.Visible = false
	var upd swag.Item
	if code := h.do(admin, "PUT", fmt.Sprintf("/api/admin/items/%d", it.ID), it, &upd); code != 200 || upd.Visible {
		t.Fatalf("update item = %d %+v", code, upd)
	}

	want := swag.Settings{RequestsOpen: false, MaxItemsPerRequest: 2, RequestCooldownDays: 0, InternationalShippingFeeCents: 2000}
	var got swag.Settings
	if code := h.do(admin, "PUT", "/api/admin/settings", want, &got); code != 200 || got != want {
		t.Fatalf("settings = %d %+v", code, got)
	}
	if code := h.do(admin, "PUT", "/api/admin/settings", swag.Settings{MaxItemsPerRequest: -1}, nil); code != 422 {
		t.Fatalf("negative settings = %d", code)
	}

	var skus struct{ SKUs []theseus.SKU }
	h.do(admin, "GET", "/api/admin/warehouse/skus", nil, &skus)
	if len(skus.SKUs) != 1 || skus.SKUs[0].SKU != "Sti/A" {
		t.Fatalf("skus = %+v", skus)
	}
}

func TestAuthorSubmissionFlow(t *testing.T) {
	h := newHarness(t)
	author, admin := h.client(authorEmail), h.client(adminEmail)

	var sub store.Submission
	body := map[string]any{"program": "Sprig", "title": "Sprig sticker", "kind": "sticker", "file_url": "https://cdn.hackclub.com/sprig.png", "quantity": 500}
	if code := h.do(author, "POST", "/api/author/submissions", body, &sub); code != 201 || sub.Status != "submitted" {
		t.Fatalf("submit = %d %+v", code, sub)
	}
	if code := h.do(h.client(userEmail), "POST", "/api/author/submissions", body, nil); code != 403 {
		t.Fatalf("non-author submit = %d", code)
	}
	if code := h.do(author, "POST", "/api/author/submissions", map[string]any{"title": "x", "file_url": "javascript:alert(1)", "quantity": 1, "program": "p"}, nil); code != 422 {
		t.Fatalf("bad url submit = %d", code)
	}

	var upd store.Submission
	if code := h.do(admin, "PUT", fmt.Sprintf("/api/admin/submissions/%d", sub.ID), map[string]any{"status": "stocked", "sku": "Sti/Spr/1", "admin_note": "done"}, &upd); code != 200 || upd.Status != "stocked" {
		t.Fatalf("update = %d %+v", code, upd)
	}
	var mine struct{ Submissions []store.Submission }
	h.do(author, "GET", "/api/author/submissions", nil, &mine)
	if len(mine.Submissions) != 1 || mine.Submissions[0].SKU != "Sti/Spr/1" {
		t.Fatalf("mine = %+v", mine)
	}
}

func newJar() http.CookieJar {
	j, _ := cookiejar.New(nil)
	return j
}
