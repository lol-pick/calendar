package reminder

import (
	"container/heap"
	"context"
	"fmt"
	"time"

	"calendar/internal/calendar"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

type Fired struct {
	EventID  int64     `json:"event_id"`
	UserID   int64     `json:"user_id"`
	Text     string    `json:"text"`
	RemindAt time.Time `json:"remind_at"`
	FiredAt  time.Time `json:"fired_at"`
}

type item struct {
	event calendar.Event
	index int
}

type itemHeap []*item

func (h itemHeap) Len() int           { return len(h) }
func (h itemHeap) Less(i, j int) bool { return h[i].event.RemindAt.Before(*h[j].event.RemindAt) }
func (h itemHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}
func (h *itemHeap) Push(x any) {
	it := x.(*item)
	it.index = len(*h)
	*h = append(*h, it)
}
func (h *itemHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return x
}

type Worker struct {
	pool   *pgxpool.Pool
	logger Logger
	in     chan calendar.Event
	now    func() time.Time
	done   chan struct{}
}

func New(pool *pgxpool.Pool, logger Logger, bufferSize int) *Worker {
	if bufferSize <= 0 {
		bufferSize = 1
	}
	return &Worker{
		pool:   pool,
		logger: logger,
		in:     make(chan calendar.Event, bufferSize),
		now:    time.Now,
		done:   make(chan struct{}),
	}
}

func (w *Worker) Schedule(e calendar.Event) bool {
	if e.RemindAt == nil {
		return false
	}
	select {
	case w.in <- e:
		return true
	default:
		return false
	}
}

func (w *Worker) Fired(ctx context.Context, userID int64) ([]Fired, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT event_id, user_id, text, remind_at, fired_at
		  FROM fired_reminders
		 WHERE user_id = $1
		 ORDER BY fired_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query fired: %w", err)
	}
	defer rows.Close()

	var out []Fired
	for rows.Next() {
		var f Fired
		if err := rows.Scan(&f.EventID, &f.UserID, &f.Text, &f.RemindAt, &f.FiredAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (w *Worker) Done() <-chan struct{} { return w.done }

func (w *Worker) Run(ctx context.Context) {
	defer close(w.done)

	h := &itemHeap{}
	heap.Init(h)

	if pending, err := w.loadPending(ctx); err != nil {
		w.logger.Errorf("reminder: load pending failed: %v", err)
	} else {
		for _, e := range pending {
			heap.Push(h, &item{event: e})
		}
		if n := len(pending); n > 0 {
			w.logger.Infof("reminder: loaded %d pending reminder(s) from DB", n)
		}
	}

	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	timerSet := false

	resetTimer := func() {
		if timerSet {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timerSet = false
		}
		if h.Len() == 0 {
			return
		}
		d := (*h)[0].event.RemindAt.Sub(w.now())
		if d < 0 {
			d = 0
		}
		timer.Reset(d)
		timerSet = true
	}

	resetTimer()

	for {
		select {
		case <-ctx.Done():
			return

		case e := <-w.in:
			heap.Push(h, &item{event: e})
			resetTimer()

		case <-timer.C:
			timerSet = false
			now := w.now()
			for h.Len() > 0 && !(*h)[0].event.RemindAt.After(now) {
				top := heap.Pop(h).(*item)
				if err := w.fire(ctx, top.event, now); err != nil {
					w.logger.Errorf("reminder: fire event_id=%d failed: %v", top.event.ID, err)
				}
			}
			resetTimer()
		}
	}
}

func (w *Worker) loadPending(ctx context.Context) ([]calendar.Event, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT id, user_id, date, text, remind_at
		  FROM events
		 WHERE remind_at IS NOT NULL AND reminder_fired = FALSE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []calendar.Event
	for rows.Next() {
		var e calendar.Event
		if err := rows.Scan(&e.ID, &e.UserID, &e.Date, &e.Text, &e.RemindAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (w *Worker) fire(ctx context.Context, e calendar.Event, now time.Time) error {
	if e.RemindAt == nil {
		return nil
	}
	return pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE events SET reminder_fired = TRUE
			 WHERE id = $1 AND reminder_fired = FALSE
		`, e.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO fired_reminders (event_id, user_id, text, remind_at, fired_at)
			VALUES ($1, $2, $3, $4, $5)
		`, e.ID, e.UserID, e.Text, *e.RemindAt, now); err != nil {
			return err
		}
		w.logger.Infof("reminder fired: user_id=%d event_id=%d text=%q remind_at=%s",
			e.UserID, e.ID, e.Text, e.RemindAt.Format(time.RFC3339))
		return nil
	})
}
