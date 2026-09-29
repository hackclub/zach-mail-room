// Package importer converts historical swag requests (Fillout form exports that
// were fulfilled through the Airtable "Warehouse" base) into this app's model.
// Imports are idempotent: each Fillout submission id is imported at most once.
package importer

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/hackclub/zach-mail-room/internal/swag"
)

// SKULine is one warehouse SKU and quantity from Fillout's custom_instructions.
type SKULine struct {
	SKU      string
	Quantity int
}

// Request is one historical request, ready to be stored.
type Request struct {
	Source           string // "fillout"
	ExternalID       string // Fillout Submission ID
	Email            string
	Name             string
	Address          swag.Address
	Lines            []SKULine
	Status           swag.Status
	ShippingFeeCents int
	PaidAt           *time.Time
	AirtableRecordID string // Airtable shipment_requests id == Zenventory order_number
	InternalNote     string // admin-only
	CreatedAt        time.Time
}

const (
	colID       = "Submission ID"
	colStarted  = "Submission started"
	colFirst    = "First Name"
	colLast     = "Last Name"
	colEmail    = "Email"
	colLine1    = "Address (Full Mailing Address)"
	colLine2    = "Address line 2 (Full Mailing Address)"
	colCity     = "City (Full Mailing Address)"
	colState    = "State/Province (Full Mailing Address)"
	colZip      = "Zip/Postal code (Full Mailing Address)"
	colCountry  = "Country (Full Mailing Address)"
	colPhone    = "Phone Number"
	colSend     = "Send To Warehouse"
	colContents = "custom_instructions"
	colNotes    = "internal_notes"
	colRecords  = "Created records"
)

var required = []string{colID, colStarted, colEmail, colLine1, colCity, colZip, colCountry, colContents}

// ParseFillout parses a Fillout results CSV. Rows that can't be imported are
// skipped with a warning rather than failing the whole file.
func ParseFillout(r io.Reader, label string) ([]Request, []string, error) {
	cr := newCSVReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read header: %w", err)
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	for _, c := range required {
		if _, ok := idx[c]; !ok {
			return nil, nil, fmt.Errorf("not a Fillout swag export: missing column %q", c)
		}
	}

	var out []Request
	var warns []string
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, warns, fmt.Errorf("line %d: %w", line, err)
		}
		get := func(col string) string {
			if i, ok := idx[col]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		id := get(colID)
		warn := func(format string, args ...any) {
			warns = append(warns, fmt.Sprintf("%s (line %d): ", id, line)+fmt.Sprintf(format, args...))
		}

		country, ok := CountryCode(get(colCountry))
		if !ok {
			warn("skipped: unknown country %q", get(colCountry))
			continue
		}
		created, err := parseFilloutTime(get(colStarted))
		if err != nil {
			warn("skipped: %v", err)
			continue
		}
		lines, bad := parseContents(get(colContents))
		if len(lines) == 0 {
			warn("skipped: no SKUs in custom_instructions %q", get(colContents))
			continue
		}

		notes := []string{"Imported from Fillout: " + label + "."}
		if len(bad) > 0 {
			warn("dropped unparseable contents %q", bad)
			notes = append(notes, fmt.Sprintf("Unparseable contents dropped: %q.", bad))
		}

		req := Request{
			Source:     "fillout",
			ExternalID: id,
			Email:      strings.ToLower(get(colEmail)),
			Name:       strings.TrimSpace(get(colFirst) + " " + get(colLast)),
			Address: swag.Address{
				FirstName: get(colFirst), LastName: get(colLast), Line1: get(colLine1), Line2: get(colLine2),
				City: get(colCity), State: get(colState), PostalCode: get(colZip), Country: country, Phone: get(colPhone),
			},
			Lines:            lines,
			AirtableRecordID: firstRecord(get(colRecords)),
			CreatedAt:        created,
			Status:           swag.StatusDispatched,
		}
		if req.Email == "" || id == "" {
			warn("skipped: missing email or submission id")
			continue
		}
		if paid := paidCents(get(colNotes)); paid > 0 {
			req.ShippingFeeCents = paid
			req.PaidAt = &created
		}
		// A row with an Airtable shipment record is already queued for the
		// warehouse; the export's Send To Warehouse flag can lag behind that.
		// Only rows with no record at all still need a decision.
		switch {
		case req.AirtableRecordID == "":
			req.Status = swag.StatusPending
			notes = append(notes, "No Airtable shipment record; review before shipping.")
		case !strings.EqualFold(get(colSend), "true"):
			notes = append(notes, "Send To Warehouse was not yet set in the export; queued via Airtable "+req.AirtableRecordID+".")
		}
		if n := get(colNotes); n != "" {
			notes = append(notes, n)
		}
		req.InternalNote = strings.Join(notes, "\n\n")
		out = append(out, req)
	}
	return out, warns, nil
}

var (
	contentTok = regexp.MustCompile(`^(\d+)\s+([A-Za-z]{3}/\S+)$`)
	paidRe     = regexp.MustCompile(`(?i)paid\s+(\d+(?:\.\d+)?)\s*USD`)
)

// parseContents parses "10 Pri/Bok/Mini25/1st, 50 Sti/Bra/O&H/Lap".
func parseContents(s string) (lines []SKULine, bad []string) {
	for _, tok := range strings.Split(s, ",") {
		t := strings.TrimSpace(tok)
		if t == "" {
			continue
		}
		m := contentTok.FindStringSubmatch(t)
		if m == nil {
			bad = append(bad, tok)
			continue
		}
		q, _ := strconv.Atoi(m[1])
		if q < 1 {
			bad = append(bad, tok)
			continue
		}
		lines = append(lines, SKULine{SKU: m[2], Quantity: q})
	}
	return lines, bad
}

func paidCents(notes string) int {
	m := paidRe.FindStringSubmatch(notes)
	if m == nil {
		return 0
	}
	f, _ := strconv.ParseFloat(m[1], 64)
	return int(f*100 + 0.5)
}

func firstRecord(s string) string {
	return strings.TrimSpace(strings.Split(s, ",")[0])
}

// parseFilloutTime parses JS Date strings: "Sat Sep 19 2026 15:03:00 GMT-0400 (Eastern Daylight Time)".
func parseFilloutTime(s string) (time.Time, error) {
	if i := strings.Index(s, " ("); i > 0 {
		s = s[:i]
	}
	t, err := time.Parse("Mon Jan 02 2006 15:04:05 GMT-0700", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad submission time %q", s)
	}
	return t.UTC(), nil
}

var countries = func() map[string]string {
	m := map[string]string{}
	names := display.English.Regions()
	for a := 'A'; a <= 'Z'; a++ {
		for b := 'A'; b <= 'Z'; b++ {
			r, err := language.ParseRegion(string([]rune{a, b}))
			if err != nil || !r.IsCountry() {
				continue
			}
			code := r.Canonicalize().String() // "UK" (reserved) -> "GB"
			m[strings.ToLower(code)] = code
			if n := names.Name(r); n != "" {
				m[strings.ToLower(n)] = code
			}
		}
	}
	for alias, code := range map[string]string{
		"usa": "US", "united states of america": "US", "u.s.a.": "US",
		"uk": "GB", "great britain": "GB", "england": "GB", "scotland": "GB", "wales": "GB",
	} {
		m[alias] = code
	}
	return m
}()

// CountryCode maps a country name (English, CLDR spelling) or ISO alpha-2 code to alpha-2.
func CountryCode(name string) (string, bool) {
	c, ok := countries[strings.ToLower(strings.TrimSpace(name))]
	return c, ok
}

// LabelFromFilename turns "Fillout Hack Club X results (5).csv" into "Hack Club X".
func LabelFromFilename(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.TrimPrefix(base, "Fillout ")
	if i := strings.Index(base, " results"); i > 0 {
		base = base[:i]
	}
	return base
}
