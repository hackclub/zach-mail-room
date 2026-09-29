package authors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAirtableIsAuthor(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v0/appBase/YSWS Authors" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer pat" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		f := r.URL.Query().Get("filterByFormula")
		if !strings.Contains(f, `{Hack Club Auth Email}`) {
			t.Errorf("formula = %q", f)
		}
		if strings.Contains(f, `"author@hackclub.com"`) {
			w.Write([]byte(`{"records":[{"id":"rec1","fields":{}}]}`))
			return
		}
		w.Write([]byte(`{"records":[]}`))
	}))
	defer srv.Close()

	d := NewAirtable(AirtableConfig{BaseURL: srv.URL, APIKey: "pat", BaseID: "appBase", Table: "YSWS Authors", EmailField: "Hack Club Auth Email", CacheTTL: time.Minute})
	ctx := context.Background()

	ok, err := d.IsAuthor(ctx, "  Author@HackClub.com ")
	if err != nil || !ok {
		t.Fatalf("IsAuthor(author) = %v, %v", ok, err)
	}
	ok, err = d.IsAuthor(ctx, "rando@example.com")
	if err != nil || ok {
		t.Fatalf("IsAuthor(rando) = %v, %v", ok, err)
	}
	// cached
	d.IsAuthor(ctx, "author@hackclub.com")
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2 (second lookup cached)", calls.Load())
	}
	if ok, _ := d.IsAuthor(ctx, ""); ok {
		t.Error("empty email must never be an author")
	}
}

func TestFormulaEscapesQuotes(t *testing.T) {
	got := formula("Email", `a"b\c@x.com`)
	want := `LOWER(TRIM({Email}))="a\"b\\c@x.com"`
	if got != want {
		t.Errorf("formula = %s, want %s", got, want)
	}
}

func TestStaticDirectory(t *testing.T) {
	d := Static{"a@x.com": true}
	if ok, _ := d.IsAuthor(context.Background(), "A@x.com"); !ok {
		t.Error("static lookup should normalize")
	}
}
