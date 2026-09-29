// Command import loads historical swag requests into the database.
//
//	go run ./cmd/import [-dry-run] [-tracking tracking.csv] "Fillout … results.csv" …
//
// Fillout exports come from the old per-item request forms, which were fulfilled
// through the Airtable warehouse base. Re-running is safe: each Fillout
// submission is imported once. The optional tracking CSV (order_number,
// tracking_number, carrier, shipped_date) backfills tracking from Zenventory.
// These files contain personal data: never commit them (this repo is public).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/hackclub/zach-mail-room/internal/db"
	"github.com/hackclub/zach-mail-room/internal/importer"
	"github.com/hackclub/zach-mail-room/internal/store"
	"github.com/hackclub/zach-mail-room/internal/theseus"
)

func main() {
	dry := flag.Bool("dry-run", false, "parse and report without writing")
	tracking := flag.String("tracking", "", "CSV of Zenventory tracking to backfill")
	flag.Parse()
	if err := run(*dry, *tracking, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "import:", err)
		os.Exit(1)
	}
}

func run(dry bool, trackingPath string, files []string) error {
	ctx := context.Background()
	var all []importer.Request
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return err
		}
		label := importer.LabelFromFilename(f)
		reqs, warns, err := importer.ParseFillout(fh, label)
		fh.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		fmt.Printf("%s: %d requests parsed, %d warnings\n", label, len(reqs), len(warns))
		for _, w := range warns {
			fmt.Println("  warning:", w)
		}
		all = append(all, reqs...)
	}
	var tracks []importer.Tracking
	if trackingPath != "" {
		fh, err := os.Open(trackingPath)
		if err != nil {
			return err
		}
		tracks, err = importer.ParseTracking(fh)
		fh.Close()
		if err != nil {
			return err
		}
		fmt.Printf("tracking: %d shipped orders\n", len(tracks))
	}
	if dry {
		fmt.Println("dry run: nothing written")
		return nil
	}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}
	if err := db.Migrate(ctx, url); err != nil {
		return err
	}
	pool, err := db.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)

	names := map[string]string{}
	if key := os.Getenv("THESEUS_API_KEY"); key != "" {
		base := os.Getenv("THESEUS_BASE_URL")
		if base == "" {
			base = "https://mail.hackclub.com"
		}
		if skus, err := theseus.New(base, key, nil).ListSKUs(ctx); err == nil {
			for _, s := range skus {
				names[s.SKU] = s.Name
			}
		} else {
			fmt.Println("warning: couldn't load SKU names from mail.hackclub.com:", err)
		}
	}

	byStatus := map[string]int{}
	created, skipped := 0, 0
	for _, r := range all {
		ok, err := st.ImportRequest(ctx, r, names)
		if err != nil {
			return fmt.Errorf("%s: %w", r.ExternalID, err)
		}
		if ok {
			created++
			byStatus[string(r.Status)]++
		} else {
			skipped++
		}
	}
	fmt.Printf("requests: %d imported, %d already present\n", created, skipped)
	keys := make([]string, 0, len(byStatus))
	for k := range byStatus {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s: %d\n", k, byStatus[k])
	}

	matched := int64(0)
	for _, t := range tracks {
		n, err := st.SetTrackingByAirtableID(ctx, t.OrderNumber, t.TrackingNumber, t.Carrier, t.ShippedAt)
		if err != nil {
			return err
		}
		matched += n
	}
	if len(tracks) > 0 {
		fmt.Printf("tracking: %d requests updated\n", matched)
	}
	return nil
}
