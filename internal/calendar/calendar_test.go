package calendar

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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

func mustDate(s string) time.Time {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCreate_AssignsIDAndStores(t *testing.T) {
	pool := setupDB(t)
	c := New(pool)
	ctx := context.Background()

	e, err := c.Create(ctx, 1, mustDate("2025-01-01"), "New Year", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.ID == 0 {
		t.Error("expected non-zero ID")
	}

	got, err := c.ForDay(ctx, 1, mustDate("2025-01-01"))
	if err != nil {
		t.Fatalf("for day: %v", err)
	}
	if len(got) != 1 || got[0].Text != "New Year" {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestCreate_WithRemindAt(t *testing.T) {
	pool := setupDB(t)
	c := New(pool)
	ctx := context.Background()
	rem := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	e, err := c.Create(ctx, 1, mustDate("2025-01-01"), "with reminder", &rem)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.RemindAt == nil || !e.RemindAt.Equal(rem) {
		t.Errorf("expected RemindAt=%v, got %v", rem, e.RemindAt)
	}
}

func TestCreate_InvalidInput(t *testing.T) {
	pool := setupDB(t)
	c := New(pool)
	ctx := context.Background()
	if _, err := c.Create(ctx, 0, mustDate("2025-01-01"), "x", nil); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestUpdate_OK(t *testing.T) {
	pool := setupDB(t)
	c := New(pool)
	ctx := context.Background()

	e, _ := c.Create(ctx, 1, mustDate("2025-01-01"), "old", nil)
	if _, err := c.Update(ctx, e.ID, 1, mustDate("2025-01-02"), "new"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := c.ForDay(ctx, 1, mustDate("2025-01-02"))
	if len(got) != 1 || got[0].Text != "new" {
		t.Errorf("update did not apply: %+v", got)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	pool := setupDB(t)
	c := New(pool)
	ctx := context.Background()
	if _, err := c.Update(ctx, 999, 1, mustDate("2025-01-01"), "x"); !errors.Is(err, ErrEventNotFound) {
		t.Errorf("expected ErrEventNotFound, got %v", err)
	}
}

func TestDelete_OK(t *testing.T) {
	pool := setupDB(t)
	c := New(pool)
	ctx := context.Background()
	e, _ := c.Create(ctx, 1, mustDate("2025-01-01"), "x", nil)
	if err := c.Delete(ctx, e.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := c.ForDay(ctx, 1, mustDate("2025-01-01"))
	if len(got) != 0 {
		t.Errorf("expected empty: %+v", got)
	}
}

func TestForWeek_BoundaryInclusive(t *testing.T) {
	pool := setupDB(t)
	c := New(pool)
	ctx := context.Background()
	_, _ = c.Create(ctx, 1, mustDate("2025-01-01"), "in1", nil)
	_, _ = c.Create(ctx, 1, mustDate("2025-01-07"), "in2", nil)
	_, _ = c.Create(ctx, 1, mustDate("2025-01-08"), "out", nil)

	got, _ := c.ForWeek(ctx, 1, mustDate("2025-01-01"))
	if len(got) != 2 {
		t.Errorf("expected 2 events, got %d: %+v", len(got), got)
	}
}

func TestMoveOlderToArchive(t *testing.T) {
	pool := setupDB(t)
	c := New(pool)
	ctx := context.Background()

	_, _ = c.Create(ctx, 1, mustDate("2025-01-01"), "old1", nil)
	_, _ = c.Create(ctx, 1, mustDate("2025-01-05"), "old2", nil)
	_, _ = c.Create(ctx, 1, mustDate("2025-01-20"), "fresh", nil)

	moved, err := c.MoveOlderToArchive(ctx, mustDate("2025-01-10"))
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved != 2 {
		t.Errorf("expected 2 moved, got %d", moved)
	}
	rest, _ := c.ForMonth(ctx, 1, mustDate("2025-01-01"))
	if len(rest) != 1 || rest[0].Text != "fresh" {
		t.Errorf("expected only 'fresh' in events, got %+v", rest)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM archive_entries`).Scan(&n); err != nil {
		t.Fatalf("count archive: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 archive entries, got %d", n)
	}
}
