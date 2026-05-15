package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"calendar/internal/archive"
	"calendar/internal/calendar"
	"calendar/internal/logger"
	"calendar/internal/reminder"
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

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	pool := setupDB(t)

	cal := calendar.New(pool)
	arc := archive.New(pool)
	log := logger.New(&bytes.Buffer{}, "[test]", 16)
	rem := reminder.New(pool, log, 16)

	ctx, cancel := context.WithCancel(context.Background())
	go rem.Run(ctx)
	t.Cleanup(func() {
		cancel()
		<-rem.Done()
		log.Close()
	})

	s := New(cal, rem, arc, log)
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	return ts
}

func decodeBody(t *testing.T, r *http.Response) map[string]any {
	t.Helper()
	defer r.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return out
}

func TestCreateAndGetForDay(t *testing.T) {
	ts := newTestServer(t)

	resp, err := http.PostForm(ts.URL+"/create_event", url.Values{
		"user_id": {"1"}, "date": {"2025-01-01"}, "event": {"New Year"},
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/events_for_day?user_id=1&date=2025-01-01")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body := decodeBody(t, resp)
	events, ok := body["result"].([]any)
	if !ok {
		t.Fatalf("expected array, got %T", body["result"])
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestCreate_BadDate_Returns400(t *testing.T) {
	ts := newTestServer(t)
	resp, _ := http.PostForm(ts.URL+"/create_event", url.Values{
		"user_id": {"1"}, "date": {"31-12-2023"}, "event": {"oops"},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreate_BadRemindAt_Returns400(t *testing.T) {
	ts := newTestServer(t)
	resp, _ := http.PostForm(ts.URL+"/create_event", url.Values{
		"user_id": {"1"}, "date": {"2025-01-01"}, "event": {"x"},
		"remind_at": {"not-a-time"},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestDelete_NotFound_Returns503(t *testing.T) {
	ts := newTestServer(t)
	resp, _ := http.PostForm(ts.URL+"/delete_event", url.Values{
		"id": {"999"}, "user_id": {"1"},
	})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

func TestUpdate_RoundTrip(t *testing.T) {
	ts := newTestServer(t)

	resp, _ := http.PostForm(ts.URL+"/create_event",
		url.Values{"user_id": {"1"}, "date": {"2025-01-01"}, "event": {"old"}})
	body := decodeBody(t, resp)
	id := int64(body["result"].(map[string]any)["id"].(float64))

	resp, _ = http.PostForm(ts.URL+"/update_event", url.Values{
		"id":      {strconv.FormatInt(id, 10)},
		"user_id": {"1"}, "date": {"2025-01-02"}, "event": {"new"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	resp, _ = http.Get(ts.URL + "/events_for_day?user_id=1&date=2025-01-02")
	body = decodeBody(t, resp)
	if len(body["result"].([]any)) != 1 {
		t.Errorf("expected 1 event after update")
	}
}

func TestCreate_WithReminder_FiresAndIsAvailableViaHTTP(t *testing.T) {
	ts := newTestServer(t)

	remindAt := time.Now().Add(50 * time.Millisecond).UTC().Format(time.RFC3339Nano)

	resp, _ := http.PostForm(ts.URL+"/create_event", url.Values{
		"user_id":   {"7"},
		"date":      {"2025-01-01"},
		"event":     {"meeting"},
		"remind_at": {remindAt},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	time.Sleep(250 * time.Millisecond)

	resp, _ = http.Get(ts.URL + "/reminders?user_id=7")
	body := decodeBody(t, resp)
	fired := body["result"].([]any)
	if len(fired) != 1 {
		t.Fatalf("expected 1 fired reminder, got %d (%+v)", len(fired), fired)
	}
}
