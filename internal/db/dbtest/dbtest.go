// Package dbtest gives each test its own freshly migrated Postgres database,
// created on the server in TEST_DATABASE_URL and dropped when the test ends.
// Every worktree shares the one dev Postgres from compose.yaml without collisions.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hackclub/zach-mail-room/internal/db"
)

// New returns a pool connected to a new, migrated, empty database.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		if os.Getenv("SKIP_DB_TESTS") != "" {
			t.Skip("SKIP_DB_TESTS set")
		}
		t.Fatal("TEST_DATABASE_URL is not set: run `make db` then `make test` (or set SKIP_DB_TESTS=1)")
	}
	ctx := context.Background()

	b := make([]byte, 6)
	rand.Read(b)
	name := "test_" + hex.EncodeToString(b)

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect %s: %v", redact(admin), err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	conn.Close(ctx)

	u, _ := url.Parse(admin)
	u.Path = "/" + name
	dsn := u.String()
	if err := db.Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(context.Background(), admin)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return pool
}

func redact(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "<unparseable dsn>"
	}
	return u.Redacted()
}
