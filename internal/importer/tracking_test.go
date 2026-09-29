package importer

import (
	"strings"
	"testing"
	"time"
)

func TestParseTracking(t *testing.T) {
	in := "order_number,tracking_number,carrier,shipped_date\nrecAAA,9400111,USPS,2026-09-21\nrecBBB,,,\nrecCCC,1Z999,UPS,\n"
	got, err := ParseTracking(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("rows without tracking should be skipped: %+v", got)
	}
	if got[0].OrderNumber != "recAAA" || got[0].TrackingNumber != "9400111" || got[0].Carrier != "USPS" ||
		got[0].ShippedAt == nil || !got[0].ShippedAt.Equal(time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("row 0 = %+v", got[0])
	}
	if got[1].ShippedAt != nil {
		t.Errorf("blank date should be nil: %+v", got[1])
	}
	if _, err := ParseTracking(strings.NewReader("a,b\n")); err == nil {
		t.Error("missing columns should error")
	}
}
