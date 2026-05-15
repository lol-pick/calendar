package archive

import (
	"context"
	"os"
	"testing"

	"calendar/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL_TEST")
	if dsn == "" {
		t.Skip("DATABASE_URL_TEST not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := storage.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := storage.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`TRUNCATE events, archive_entries, fired_reminders RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestArchive_List(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO archive_entries (original_event_id, user_id, date, text)
		VALUES (10, 1, '2025-01-01', 'older'),
		       (11, 1, '2025-02-01', 'newer'),
		       (12, 2, '2025-01-15', 'another user')
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	a := New(pool)
	mine, err := a.List(ctx, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(mine) != 2 {
		t.Fatalf("expected 2 entries for user 1, got %d", len(mine))
	}
	if mine[0].Text != "newer" {
		t.Errorf("expected 'newer' first, got %q", mine[0].Text)
	}

	none, err := a.List(ctx, 999)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if none != nil {
		t.Errorf("expected nil, got %+v", none)
	}
}
