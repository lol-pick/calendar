package storage

import (
	"context"
	"os"
	"testing"
)

func TestMigrate_Idempotent(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL_TEST")
	if dsn == "" {
		t.Skip("DATABASE_URL_TEST not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pool.Close()

	for _, q := range []string{
		`DROP TABLE IF EXISTS schema_migrations`,
		`DROP TABLE IF EXISTS fired_reminders`,
		`DROP TABLE IF EXISTS archive_entries`,
		`DROP TABLE IF EXISTS events`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate 1: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate 2 (should be no-op): %v", err)
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables
		   WHERE table_name IN ('events','archive_entries','fired_reminders')`,
	).Scan(&n); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 tables, got %d", n)
	}
}
