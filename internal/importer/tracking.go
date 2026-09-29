package importer

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Tracking is one shipped Zenventory order, exported from the data warehouse:
//
//	SELECT o.order_number, s.tracking_number, s.carrier, s.shipped_date
//	FROM agh_fulfillment_zenventory.customer_orders o
//	JOIN agh_fulfillment_zenventory.shipments s USING (order_number) WHERE ...
type Tracking struct {
	OrderNumber    string // == Airtable shipment_requests record id
	TrackingNumber string
	Carrier        string
	ShippedAt      *time.Time
}

func ParseTracking(r io.Reader) ([]Tracking, error) {
	cr := newCSVReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	for _, c := range []string{"order_number", "tracking_number", "carrier", "shipped_date"} {
		if _, ok := idx[c]; !ok {
			return nil, fmt.Errorf("tracking csv: missing column %q", c)
		}
	}
	var out []Tracking
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		t := Tracking{
			OrderNumber:    strings.TrimSpace(rec[idx["order_number"]]),
			TrackingNumber: strings.TrimSpace(rec[idx["tracking_number"]]),
			Carrier:        strings.TrimSpace(rec[idx["carrier"]]),
		}
		if t.OrderNumber == "" || t.TrackingNumber == "" {
			continue
		}
		if d := strings.TrimSpace(rec[idx["shipped_date"]]); d != "" {
			if ts, err := time.Parse("2006-01-02", d[:min(len(d), 10)]); err == nil {
				t.ShippedAt = &ts
			}
		}
		out = append(out, t)
	}
}
