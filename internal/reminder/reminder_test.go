package reminder

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"calendar/internal/calendar"
	"calendar/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fakeLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *fakeLogger) push(s string)             { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }
func (l *fakeLogger) Infof(f string, _ ...any)  { l.push(f) }
func (l *fakeLogger) Warnf(f string, _ ...any)  { l.push(f) }
func (l *fakeLogger) Errorf(f string, _ ...any) { l.push(f) }

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

func TestWorker_FiresAndPersists(t *testing.T) {
	pool := setupDB(t)
	cal := calendar.New(pool)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := New(pool, &fakeLogger{}, 8)
	go w.Run(ctx)

	rem := time.Now().Add(40 * time.Millisecond).UTC()
	e, err := cal.Create(ctx, 1, time.Now(), "meeting", &rem)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	w.Schedule(e)

	time.Sleep(200 * time.Millisecond)

	fired, err := w.Fired(ctx, 1)
	if err != nil {
		t.Fatalf("fired: %v", err)
	}
	if len(fired) != 1 {
		t.Fatalf("expected 1 fired, got %d", len(fired))
	}
	if fired[0].EventID != e.ID {
		t.Errorf("expected event id %d, got %d", e.ID, fired[0].EventID)
	}

	var done bool
	if err := pool.QueryRow(ctx,
		`SELECT reminder_fired FROM events WHERE id = $1`, e.ID).Scan(&done); err != nil {
		t.Fatalf("check flag: %v", err)
	}
	if !done {
		t.Error("expected reminder_fired = true")
	}
}

func TestWorker_LoadPendingOnStart(t *testing.T) {
	pool := setupDB(t)
	cal := calendar.New(pool)
	ctx := context.Background()

	rem := time.Now().Add(40 * time.Millisecond).UTC()
	e, err := cal.Create(ctx, 7, time.Now(), "after restart", &rem)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	wctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := New(pool, &fakeLogger{}, 8)
	go w.Run(wctx)

	time.Sleep(200 * time.Millisecond)

	fired, _ := w.Fired(ctx, 7)
	if len(fired) != 1 || fired[0].EventID != e.ID {
		t.Errorf("expected 1 fired (id=%d), got %+v", e.ID, fired)
	}
}

func TestWorker_NoRemindAtIsIgnored(t *testing.T) {
	w := New(nil, &fakeLogger{}, 1)
	if ok := w.Schedule(calendar.Event{ID: 1, UserID: 1, Text: "no remind"}); ok {
		t.Errorf("expected Schedule to return false for nil RemindAt")
	}
}

func TestWorker_GracefulShutdown(t *testing.T) {
	pool := setupDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	w := New(pool, &fakeLogger{}, 1)
	go w.Run(ctx)

	cancel()
	select {
	case <-w.Done():
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after ctx cancel")
	}
}
