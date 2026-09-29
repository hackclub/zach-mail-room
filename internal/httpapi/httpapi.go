// Package httpapi wires HTTP routes: Hack Club Auth login, the JSON API for
// the three user flows (hack clubbers, YSWS authors, admin), and the embedded
// PWA frontend.
package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/hackclub/zach-mail-room/internal/auth"
	"github.com/hackclub/zach-mail-room/internal/authors"
	"github.com/hackclub/zach-mail-room/internal/config"
	"github.com/hackclub/zach-mail-room/internal/store"
	"github.com/hackclub/zach-mail-room/internal/swag"
	"github.com/hackclub/zach-mail-room/internal/theseus"
)

const (
	sessionCookie = "zmr_session"
	stateCookie   = "zmr_oauth_state"
	sessionTTL    = 30 * 24 * time.Hour
)

// Warehouse is the subset of the mail.hackclub.com client the API uses.
type Warehouse interface {
	ListSKUs(ctx context.Context) ([]theseus.SKU, error)
	CreateOrder(ctx context.Context, in theseus.OrderInput) (*theseus.Order, error)
	GetOrder(ctx context.Context, id string) (*theseus.Order, error)
}

type Deps struct {
	Config    *config.Config
	Store     *store.Store
	Provider  auth.Provider
	Authors   authors.Directory
	Warehouse Warehouse
	Static    fs.FS // built PWA (web/build); nil disables static serving
	Now       func() time.Time
	Log       *slog.Logger
}

type server struct {
	Deps
}

func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	s := &server{d}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /auth/login", s.login)
	mux.HandleFunc("GET /auth/callback", s.callback)
	mux.HandleFunc("POST /auth/logout", s.logout)

	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("GET /api/catalog", s.catalog)

	// hack clubbers
	mux.Handle("GET /api/requests", s.requireUser(s.myRequests))
	mux.Handle("POST /api/requests", s.requireUser(s.createRequest))
	mux.Handle("POST /api/requests/{id}/cancel", s.requireUser(s.cancelRequest))

	// YSWS authors
	mux.Handle("GET /api/author/submissions", s.requireAuthor(s.mySubmissions))
	mux.Handle("POST /api/author/submissions", s.requireAuthor(s.createSubmission))

	// admin
	mux.Handle("GET /api/admin/requests", s.requireAdmin(s.adminRequests))
	mux.Handle("POST /api/admin/requests/{id}/mark-paid", s.requireAdmin(s.adminMarkPaid))
	mux.Handle("POST /api/admin/requests/{id}/dispatch", s.requireAdmin(s.adminDispatch))
	mux.Handle("POST /api/admin/requests/{id}/reject", s.requireAdmin(s.adminReject))
	mux.Handle("POST /api/admin/requests/{id}/refresh", s.requireAdmin(s.adminRefresh))
	mux.Handle("GET /api/admin/items", s.requireAdmin(s.adminItems))
	mux.Handle("POST /api/admin/items", s.requireAdmin(s.adminCreateItem))
	mux.Handle("PUT /api/admin/items/{id}", s.requireAdmin(s.adminUpdateItem))
	mux.Handle("GET /api/admin/warehouse/skus", s.requireAdmin(s.adminSKUs))
	mux.Handle("GET /api/admin/settings", s.requireAdmin(s.adminSettings))
	mux.Handle("PUT /api/admin/settings", s.requireAdmin(s.adminUpdateSettings))
	mux.Handle("GET /api/admin/submissions", s.requireAdmin(s.adminSubmissions))
	mux.Handle("PUT /api/admin/submissions/{id}", s.requireAdmin(s.adminUpdateSubmission))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotFound, "not found") })
	mux.HandleFunc("/", s.static)

	return s.csrf(mux)
}

// ---- middleware ----

// csrf rejects state-changing API calls that aren't same-origin JSON. Browsers
// can't send cross-site application/json without a CORS preflight we never
// grant, and SameSite=Lax session cookies cover the rest.
func (s *server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && strings.HasPrefix(r.URL.Path, "/api/") {
			if o := r.Header.Get("Origin"); o != "" && o != s.Config.BaseURL {
				writeErr(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" {
				writeErr(w, http.StatusUnsupportedMediaType, "send application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type ctxKey struct{}

type viewer struct {
	user  *store.User
	roles roles
}

type roles struct {
	Author bool `json:"author"`
	Admin  bool `json:"admin"`
}

func (s *server) currentUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := s.Store.UserBySession(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	return u
}

func (s *server) rolesFor(ctx context.Context, u *store.User) roles {
	if u == nil {
		return roles{}
	}
	if s.Config.IsAdminEmail(u.Email) {
		return roles{Admin: true, Author: true}
	}
	ok, err := s.Authors.IsAuthor(ctx, u.Email)
	if err != nil {
		s.Log.Warn("ysws author lookup failed", "err", err)
	}
	return roles{Author: ok}
}

type handler func(w http.ResponseWriter, r *http.Request, v viewer)

func (s *server) requireRole(need func(roles) bool, h handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "sign in first")
			return
		}
		v := viewer{user: u, roles: s.rolesFor(r.Context(), u)}
		if !need(v.roles) {
			writeErr(w, http.StatusForbidden, "you don't have access to this")
			return
		}
		h(w, r, v)
	})
}

func (s *server) requireUser(h handler) http.Handler {
	return s.requireRole(func(roles) bool { return true }, h)
}
func (s *server) requireAuthor(h handler) http.Handler {
	return s.requireRole(func(r roles) bool { return r.Author }, h)
}
func (s *server) requireAdmin(h handler) http.Handler {
	return s.requireRole(func(r roles) bool { return r.Admin }, h)
}

// ---- auth ----

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 24)
	rand.Read(b)
	state := base64.RawURLEncoding.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: state, Path: "/auth", MaxAge: 600,
		HttpOnly: true, Secure: s.Config.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.Provider.AuthCodeURL(state), http.StatusFound)
}

func (s *server) callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(stateCookie)
	state := r.URL.Query().Get("state")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		http.Error(w, "login expired or invalid state — try again", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Path: "/auth", MaxAge: -1})
	if e := r.URL.Query().Get("error"); e != "" {
		http.Error(w, "sign-in was cancelled: "+e, http.StatusBadRequest)
		return
	}
	id, err := s.Provider.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		s.Log.Warn("hca exchange failed", "err", err)
		http.Error(w, "couldn't sign you in with Hack Club Auth", http.StatusBadGateway)
		return
	}
	u, err := s.Store.UpsertUser(r.Context(), *id)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	tok, err := s.Store.CreateSession(r.Context(), u.ID, sessionTTL)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: s.Config.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.Store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

type meResponse struct {
	User  *store.User `json:"user"`
	Roles roles       `json:"roles"`
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	writeJSON(w, http.StatusOK, meResponse{User: u, Roles: s.rolesFor(r.Context(), u)})
}

// ---- hack clubber flow ----

func (s *server) catalog(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListItems(r.Context(), true)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"settings": map[string]any{
			"requests_open":                    st.RequestsOpen,
			"max_items_per_request":            st.MaxItemsPerRequest,
			"request_cooldown_days":            st.RequestCooldownDays,
			"international_shipping_fee_cents": st.InternationalShippingFeeCents,
			"hcb_payment_url":                  s.Config.HCBShippingPaymentURL,
		},
	})
}

type requestView struct {
	*store.Request
	PaymentURL string `json:"payment_url,omitempty"`
}

func (s *server) view(r *store.Request) requestView {
	v := requestView{Request: r}
	if r.Status == swag.StatusAwaitingPayment && s.Config.HCBShippingPaymentURL != "" {
		q := url.Values{}
		q.Set("amount", strconv.Itoa(r.ShippingFeeCents))
		q.Set("message", "Shipping for swag request #"+strconv.FormatInt(r.ID, 10))
		sep := "?"
		if strings.Contains(s.Config.HCBShippingPaymentURL, "?") {
			sep = "&"
		}
		v.PaymentURL = s.Config.HCBShippingPaymentURL + sep + q.Encode()
	}
	return v
}

// userView is what a requester sees: no admin-only notes.
func (s *server) userView(r *store.Request) requestView {
	cp := *r
	cp.InternalNote = ""
	return s.view(&cp)
}

func (s *server) views(rs []*store.Request) []requestView {
	out := make([]requestView, len(rs))
	for i, r := range rs {
		out[i] = s.view(r)
	}
	return out
}

func (s *server) myRequests(w http.ResponseWriter, r *http.Request, v viewer) {
	rs, err := s.Store.ListRequestsByUser(r.Context(), v.user.ID)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	out := make([]requestView, len(rs))
	for i, r := range rs {
		out[i] = s.userView(r)
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

func (s *server) createRequest(w http.ResponseWriter, r *http.Request, v viewer) {
	var body struct {
		Address swag.Address `json:"address"`
		Lines   []swag.Line  `json:"lines"`
		Note    string       `json:"note"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	body.Address.Country = strings.ToUpper(strings.TrimSpace(body.Address.Country))
	if err := body.Address.Validate(); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(body.Note) > 1000 {
		writeErr(w, http.StatusUnprocessableEntity, "note is too long")
		return
	}
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	items, err := s.Store.ListItems(r.Context(), true)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	cat := map[int64]swag.Item{}
	for _, it := range items {
		cat[it.ID] = it
	}
	status, fee := swag.InitialStatus(st, body.Address.Country)
	req, err := s.Store.CreateRequest(r.Context(), store.NewRequest{
		UserID: v.user.ID, Address: body.Address, Lines: body.Lines, Status: status, ShippingFeeCents: fee, Note: body.Note,
	}, func(prior map[int64]int, last *time.Time) error {
		return swag.Validate(swag.ValidateInput{
			Settings: st, Catalog: cat, Lines: body.Lines, PriorQuantities: prior, LastRequestAt: last, Now: s.Now(),
		})
	})
	if err != nil {
		if isRuleErr(err) {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.userView(req))
}

func isRuleErr(err error) bool {
	for _, e := range []error{swag.ErrRequestsClosed, swag.ErrNoItems, swag.ErrBadQuantity, swag.ErrUnknownItem,
		swag.ErrDuplicateItem, swag.ErrItemLimit, swag.ErrTooManyItems, swag.ErrCooldown, swag.ErrBadAddress} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func (s *server) cancelRequest(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, err := s.Store.GetRequest(r.Context(), id)
	if err != nil || req.UserID != v.user.ID {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	}
	req, err = s.Store.TransitionRequest(r.Context(), id, swag.StatusCancelled, "")
	if err == nil {
		req.InternalNote = ""
	}
	s.respondRequest(w, req, err)
}

// ---- YSWS author flow ----

func (s *server) mySubmissions(w http.ResponseWriter, r *http.Request, v viewer) {
	subs, err := s.Store.ListSubmissionsByUser(r.Context(), v.user.ID)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"submissions": subs})
}

func (s *server) createSubmission(w http.ResponseWriter, r *http.Request, v viewer) {
	var body store.Submission
	if !readJSON(w, r, &body) {
		return
	}
	body.Program, body.Title = strings.TrimSpace(body.Program), strings.TrimSpace(body.Title)
	if body.Kind == "" {
		body.Kind = "sticker"
	}
	switch {
	case body.Program == "" || body.Title == "":
		writeErr(w, http.StatusUnprocessableEntity, "program and title are required")
		return
	case !isHTTPURL(body.FileURL):
		writeErr(w, http.StatusUnprocessableEntity, "file_url must be an http(s) link to the print-ready file")
		return
	case body.Quantity < 1 || body.Quantity > 100000:
		writeErr(w, http.StatusUnprocessableEntity, "quantity must be between 1 and 100000")
		return
	}
	body.UserID = v.user.ID
	sub, err := s.Store.CreateSubmission(r.Context(), body)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sub)
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// ---- admin ----

func (s *server) adminRequests(w http.ResponseWriter, r *http.Request, v viewer) {
	rs, err := s.Store.ListRequests(r.Context(), swag.Status(r.URL.Query().Get("status")))
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": s.views(rs)})
}

func (s *server) adminMarkPaid(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.respond(w)(s.Store.MarkPaid(r.Context(), id))
}

func (s *server) adminReject(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	s.respond(w)(s.Store.TransitionRequest(r.Context(), id, swag.StatusRejected, body.Note))
}

// adminDispatch creates the warehouse order in mail.hackclub.com. The
// idempotency key is derived from the request id, so a retry after a timeout
// replays the same Theseus order instead of shipping twice.
func (s *server) adminDispatch(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, err := s.Store.GetRequest(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	if !swag.CanTransition(req.Status, swag.StatusDispatched) {
		writeErr(w, http.StatusConflict, "request is "+string(req.Status)+"; only pending requests can be dispatched")
		return
	}
	contents := make([]theseus.Content, len(req.Lines))
	for i, l := range req.Lines {
		contents[i] = theseus.Content{SKU: l.SKU, Quantity: l.Quantity}
	}
	a := req.Address
	order, err := s.Warehouse.CreateOrder(r.Context(), theseus.OrderInput{
		RecipientEmail: req.UserEmail,
		Title:          "Hack Club swag",
		IdempotencyKey: s.Config.TheseusOrderTag + "-request-" + strconv.FormatInt(req.ID, 10),
		Tags:           []string{s.Config.TheseusOrderTag},
		Metadata:       map[string]any{"zach_mail_room_request_id": req.ID},
		Address: theseus.Address{
			FirstName: a.FirstName, LastName: a.LastName, Line1: a.Line1, Line2: a.Line2, City: a.City,
			State: a.State, PostalCode: a.PostalCode, Country: a.Country, Phone: a.Phone,
		},
		Contents: contents,
	})
	if err != nil {
		s.Log.Error("theseus dispatch failed", "request_id", req.ID, "err", err)
		writeErr(w, http.StatusBadGateway, "mail.hackclub.com refused the order: "+err.Error())
		return
	}
	s.respond(w)(s.Store.MarkDispatched(r.Context(), id, order.ID))
}

func (s *server) adminRefresh(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, err := s.Store.GetRequest(r.Context(), id)
	if err != nil {
		s.respond(w)(nil, err)
		return
	}
	if req.TheseusOrderID == "" {
		writeErr(w, http.StatusConflict, "request has not been dispatched")
		return
	}
	o, err := s.Warehouse.GetOrder(r.Context(), req.TheseusOrderID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.respond(w)(s.Store.SetTracking(r.Context(), id, o.TrackingNumber, o.Carrier))
}

func (s *server) respond(w http.ResponseWriter) func(*store.Request, error) {
	return func(req *store.Request, err error) { s.respondRequest(w, req, err) }
}

func (s *server) respondRequest(w http.ResponseWriter, req *store.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "request not found")
	case errors.Is(err, store.ErrBadTransition):
		writeErr(w, http.StatusConflict, err.Error())
	case err != nil:
		s.serverErr(w, err)
	default:
		writeJSON(w, http.StatusOK, s.view(req))
	}
}

func (s *server) adminItems(w http.ResponseWriter, r *http.Request, v viewer) {
	items, err := s.Store.ListItems(r.Context(), false)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func validItem(it swag.Item) string {
	switch {
	case strings.TrimSpace(it.SKU) == "" || strings.TrimSpace(it.Name) == "":
		return "sku and name are required"
	case it.MaxPerRequest < 1:
		return "max_per_request must be at least 1"
	case it.MaxPerUser != nil && *it.MaxPerUser < 0:
		return "max_per_user can't be negative"
	case it.ImageURL != "" && !isHTTPURL(it.ImageURL):
		return "image_url must be an http(s) URL"
	}
	return ""
}

func (s *server) adminCreateItem(w http.ResponseWriter, r *http.Request, v viewer) {
	var it swag.Item
	if !readJSON(w, r, &it) {
		return
	}
	if msg := validItem(it); msg != "" {
		writeErr(w, http.StatusUnprocessableEntity, msg)
		return
	}
	created, err := s.Store.CreateItem(r.Context(), it)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "an item with that SKU is already listed")
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *server) adminUpdateItem(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var it swag.Item
	if !readJSON(w, r, &it) {
		return
	}
	it.ID = id
	if msg := validItem(it); msg != "" {
		writeErr(w, http.StatusUnprocessableEntity, msg)
		return
	}
	upd, err := s.Store.UpdateItem(r.Context(), it)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "item not found")
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "an item with that SKU is already listed")
	case err != nil:
		s.serverErr(w, err)
	default:
		writeJSON(w, http.StatusOK, upd)
	}
}

func (s *server) adminSKUs(w http.ResponseWriter, r *http.Request, v viewer) {
	skus, err := s.Warehouse.ListSKUs(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skus": skus})
}

func (s *server) adminSettings(w http.ResponseWriter, r *http.Request, v viewer) {
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *server) adminUpdateSettings(w http.ResponseWriter, r *http.Request, v viewer) {
	var st swag.Settings
	if !readJSON(w, r, &st) {
		return
	}
	if st.MaxItemsPerRequest < 0 || st.RequestCooldownDays < 0 || st.InternationalShippingFeeCents < 0 {
		writeErr(w, http.StatusUnprocessableEntity, "limits can't be negative")
		return
	}
	if err := s.Store.UpdateSettings(r.Context(), st); err != nil {
		s.serverErr(w, err)
		return
	}
	s.adminSettings(w, r, v)
}

func (s *server) adminSubmissions(w http.ResponseWriter, r *http.Request, v viewer) {
	subs, err := s.Store.ListSubmissions(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"submissions": subs})
}

var submissionStatuses = map[string]bool{"submitted": true, "approved": true, "printing": true, "stocked": true, "rejected": true}

func (s *server) adminUpdateSubmission(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Status    string `json:"status"`
		AdminNote string `json:"admin_note"`
		SKU       string `json:"sku"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !submissionStatuses[body.Status] {
		writeErr(w, http.StatusUnprocessableEntity, "unknown status")
		return
	}
	sub, err := s.Store.UpdateSubmission(r.Context(), id, body.Status, body.AdminNote, strings.TrimSpace(body.SKU))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "submission not found")
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// ---- static PWA ----

func (s *server) static(w http.ResponseWriter, r *http.Request) {
	if s.Static == nil {
		http.NotFound(w, r)
		return
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}
	if st, err := fs.Stat(s.Static, p); err != nil || st.IsDir() {
		p = "index.html" // SPA fallback: client-side router handles the path
	}
	switch {
	case p == "index.html" || p == "service-worker.js" || p == "sw.js" || strings.HasSuffix(p, ".webmanifest"):
		w.Header().Set("Cache-Control", "no-cache")
	case strings.HasPrefix(p, "_app/immutable/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, s.Static, p)
}

// ---- helpers ----

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeErr(w, http.StatusBadRequest, "bad id")
		return 0, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *server) serverErr(w http.ResponseWriter, err error) {
	s.Log.Error("internal error", "err", err)
	writeErr(w, http.StatusInternalServerError, "something went wrong")
}
