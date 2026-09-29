package importer

import (
	"strings"
	"testing"
	"time"

	"github.com/hackclub/zach-mail-room/internal/swag"
)

// Synthetic rows shaped like real Fillout exports (no real people).
const filloutCSV = "\ufeff" + `Submission ID,Last updated,Submission started,Status,Current step,Created records,Global shipping (Stripe),How many mini-magazines can we send you?,First Name,Last Name,Email,Address (Full Mailing Address),Address line 2 (Full Mailing Address),City (Full Mailing Address),State/Province (Full Mailing Address),Zip/Postal code (Full Mailing Address),Country (Full Mailing Address),Phone Number,Send To Warehouse,custom_instructions,internal_notes,Errors,Url,Network ID
sub-us,"Sat Sep 19 2026 15:05:00 GMT-0400 (Eastern Daylight Time)","Sat Sep 19 2026 15:03:00 GMT-0400 (Eastern Daylight Time)",finished,Ending,recAAA,"Amount: $0.00",10,Orpheus,Dino, Orpheus@Example.COM ,15 Falls Rd,Apt 2,Shelburne,Vermont,05482,United States,,true,"10 Pri/Bok/Mini25/1st, 50 Sti/Bra/O&H/Lap","Requested via https://forms.hackclub.com/t/x

Requestor paid $0. USA shipment.",None,u,n
sub-intl,"Mon Sep 28 2026 21:10:00 GMT-0400 (Eastern Daylight Time)","Mon Sep 28 2026 21:10:00 GMT-0400 (Eastern Daylight Time)",finished,Ending,recBBB,View Payment,50,Heidi,Hakkuun,heidi@example.com,1 Rue X,,Saint-Denis,,97400,Réunion,+262 1234,true,50 Pri/Bok/Mini25/1st,"Requestor paid 15 USD. Payment URL: https://dashboard.stripe.com/payments/pi_1",None,u,n
sub-unsent,"Mon Sep 28 2026 21:10:00 GMT-0400 (Eastern Daylight Time)","Mon Sep 28 2026 21:10:00 GMT-0400 (Eastern Daylight Time)",finished,Ending,recCCC,"Amount: $0.00",5,A,B,a@example.com,1 Main,,Burlington,VT,05401,United States,,,"5 Pri/Bok/Mini25/1st",notes,None,u,n
sub-bad-sku,"Mon Sep 28 2026 21:10:00 GMT-0400 (Eastern Daylight Time)","Mon Sep 28 2026 21:10:00 GMT-0400 (Eastern Daylight Time)",finished,Ending,recDDD,"Amount: $0.00",2,C,D,c@example.com,1 Main,,Burlington,VT,05401,United States,,true,"2 , 1 Swa/HC/Pin/1st",notes,None,u,n
sub-bad-country,"Mon Sep 28 2026 21:10:00 GMT-0400 (Eastern Daylight Time)","Mon Sep 28 2026 21:10:00 GMT-0400 (Eastern Daylight Time)",finished,Ending,recEEE,"Amount: $0.00",2,E,F,e@example.com,1 Main,,X,,1,Atlantis,,true,"2 Pri/Bok/Mini25/1st",notes,None,u,n
`

func TestParseFillout(t *testing.T) {
	reqs, warns, err := ParseFillout(strings.NewReader(filloutCSV), "Mini-Magazine Request")
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 4 {
		t.Fatalf("got %d requests, want 4 (Atlantis skipped); warnings=%v", len(reqs), warns)
	}

	us := reqs[0]
	if us.ExternalID != "sub-us" || us.Email != "orpheus@example.com" || us.Name != "Orpheus Dino" {
		t.Errorf("identity = %+v", us)
	}
	wantTime := time.Date(2026, 9, 19, 19, 3, 0, 0, time.UTC)
	if !us.CreatedAt.Equal(wantTime) {
		t.Errorf("CreatedAt = %v, want %v", us.CreatedAt, wantTime)
	}
	wantAddr := swag.Address{FirstName: "Orpheus", LastName: "Dino", Line1: "15 Falls Rd", Line2: "Apt 2", City: "Shelburne", State: "Vermont", PostalCode: "05482", Country: "US"}
	if us.Address != wantAddr {
		t.Errorf("address = %+v", us.Address)
	}
	if len(us.Lines) != 2 || us.Lines[0] != (SKULine{"Pri/Bok/Mini25/1st", 10}) || us.Lines[1] != (SKULine{"Sti/Bra/O&H/Lap", 50}) {
		t.Errorf("lines = %+v", us.Lines)
	}
	if us.Status != swag.StatusDispatched || us.AirtableRecordID != "recAAA" || us.ShippingFeeCents != 0 || us.PaidAt != nil {
		t.Errorf("status fields = %+v", us)
	}
	if !strings.Contains(us.InternalNote, "Mini-Magazine Request") || !strings.Contains(us.InternalNote, "USA shipment") {
		t.Errorf("admin note = %q", us.InternalNote)
	}

	intl := reqs[1]
	if intl.Address.Country != "RE" || intl.Address.Phone != "+262 1234" || intl.ShippingFeeCents != 1500 || intl.PaidAt == nil {
		t.Errorf("intl = %+v", intl)
	}

	if reqs[2].Status != swag.StatusPending || !strings.Contains(reqs[2].InternalNote, "never sent to the warehouse") {
		t.Errorf("unsent = %+v", reqs[2])
	}

	bad := reqs[3]
	if len(bad.Lines) != 1 || bad.Lines[0].SKU != "Swa/HC/Pin/1st" || !strings.Contains(bad.InternalNote, `"2 "`) {
		t.Errorf("unparseable token should be dropped and noted: %+v", bad)
	}

	joined := strings.Join(warns, "\n")
	if !strings.Contains(joined, "sub-bad-country") || !strings.Contains(joined, "Atlantis") || !strings.Contains(joined, "sub-bad-sku") {
		t.Errorf("warnings = %v", warns)
	}
}

func TestParseFilloutRequiresColumns(t *testing.T) {
	_, _, err := ParseFillout(strings.NewReader("a,b\n1,2\n"), "x")
	if err == nil || !strings.Contains(err.Error(), "Submission ID") {
		t.Fatalf("err = %v", err)
	}
}

func TestCountryCode(t *testing.T) {
	for in, want := range map[string]string{
		"United States": "US", "united states of america": "US", "USA": "US", "US": "US",
		"United Kingdom": "GB", "UK": "GB", "Réunion": "RE", "Netherlands": "NL",
		"United Arab Emirates": "AE", "New Zealand": "NZ", "Croatia": "HR",
	} {
		if got, ok := CountryCode(in); !ok || got != want {
			t.Errorf("CountryCode(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := CountryCode("Atlantis"); ok {
		t.Error("Atlantis should not resolve")
	}
}

func TestLabelFromFilename(t *testing.T) {
	for in, want := range map[string]string{
		"/x/Fillout Hack Club Mini-Magazine Request results (5).csv": "Hack Club Mini-Magazine Request",
		"Fillout Hack Club Staff T-Shirt Request results.csv":        "Hack Club Staff T-Shirt Request",
		"other.csv": "other",
	} {
		if got := LabelFromFilename(in); got != want {
			t.Errorf("LabelFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseFilloutToleratesBareQuotesInHeader(t *testing.T) {
	// Real exports have long question columns with unescaped quotes, e.g. "it's "free"".
	in := strings.Replace(filloutCSV, "Errors,Url", `Say "yes" please,Errors,Url`, 1)
	in = strings.ReplaceAll(in, ",None,u,n", ",x,None,u,n")
	reqs, _, err := ParseFillout(strings.NewReader(in), "x")
	if err != nil || len(reqs) != 4 {
		t.Fatalf("got %d, %v", len(reqs), err)
	}
}

func TestParseFilloutQuotedHeaderAfterBOM(t *testing.T) {
	// Real exports start with a BOM immediately followed by a quoted header.
	in := strings.Replace(filloutCSV, "\ufeffSubmission ID,Last updated", "\ufeff\"Submission ID\",\"Last updated\"", 1)
	reqs, _, err := ParseFillout(strings.NewReader(in), "x")
	if err != nil || len(reqs) != 4 {
		t.Fatalf("got %d, %v", len(reqs), err)
	}
}
