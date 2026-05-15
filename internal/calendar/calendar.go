package calendar

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DateLayout = "2006-01-02"

var (
	ErrInvalidInput  = errors.New("calendar: invalid input")
	ErrEventNotFound = errors.New("calendar: event not found")
)

type Event struct {
	ID       int64      `json:"id"`
	UserID   int64      `json:"user_id"`
	Date     time.Time  `json:"date"`
	Text     string     `json:"text"`
	RemindAt *time.Time `json:"remind_at,omitempty"`
}

type Calendar struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Calendar {
	return &Calendar{pool: pool}
}

func (c *Calendar) Create(ctx context.Context, userID int64, date time.Time, text string, remindAt *time.Time) (Event, error) {
	if userID <= 0 || text == "" {
		return Event{}, ErrInvalidInput
	}
	date = truncateDate(date)

	var id int64
	err := c.pool.QueryRow(ctx, `
		INSERT INTO events (user_id, date, text, remind_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, userID, date, text, remindAt).Scan(&id)
	if err != nil {
		return Event{}, fmt.Errorf("insert event: %w", err)
	}
	return Event{ID: id, UserID: userID, Date: date, Text: text, RemindAt: remindAt}, nil
}

func (c *Calendar) Update(ctx context.Context, id, userID int64, date time.Time, text string) (Event, error) {
	if id <= 0 || userID <= 0 || text == "" {
		return Event{}, ErrInvalidInput
	}
	date = truncateDate(date)

	tag, err := c.pool.Exec(ctx, `
		UPDATE events SET date = $1, text = $2
		 WHERE id = $3 AND user_id = $4
	`, date, text, id, userID)
	if err != nil {
		return Event{}, fmt.Errorf("update event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Event{}, ErrEventNotFound
	}

	return c.byID(ctx, id)
}

func (c *Calendar) Delete(ctx context.Context, id, userID int64) error {
	if id <= 0 || userID <= 0 {
		return ErrInvalidInput
	}
	tag, err := c.pool.Exec(ctx,
		`DELETE FROM events WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("delete event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrEventNotFound
	}
	return nil
}

func (c *Calendar) ForDay(ctx context.Context, userID int64, day time.Time) ([]Event, error) {
	from := truncateDate(day)
	return c.rangeQuery(ctx, userID, from, from.AddDate(0, 0, 1))
}
func (c *Calendar) ForWeek(ctx context.Context, userID int64, day time.Time) ([]Event, error) {
	from := truncateDate(day)
	return c.rangeQuery(ctx, userID, from, from.AddDate(0, 0, 7))
}
func (c *Calendar) ForMonth(ctx context.Context, userID int64, day time.Time) ([]Event, error) {
	from := truncateDate(day)
	return c.rangeQuery(ctx, userID, from, from.AddDate(0, 1, 0))
}

func (c *Calendar) MoveOlderToArchive(ctx context.Context, cutoff time.Time) (int, error) {
	var moved int
	err := pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		// CTE «moved» удаляет старые из events и возвращает их строки;
		// мы тут же INSERT-им их в archive_entries.
		tag, err := tx.Exec(ctx, `
			WITH moved AS (
				DELETE FROM events
				 WHERE date < $1
				RETURNING id, user_id, date, text, remind_at
			)
			INSERT INTO archive_entries (original_event_id, user_id, date, text, remind_at)
			SELECT id, user_id, date, text, remind_at FROM moved
		`, cutoff)
		if err != nil {
			return err
		}
		moved = int(tag.RowsAffected())
		return nil
	})
	return moved, err
}

func (c *Calendar) rangeQuery(ctx context.Context, userID int64, from, to time.Time) ([]Event, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT id, user_id, date, text, remind_at
		  FROM events
		 WHERE user_id = $1 AND date >= $2 AND date < $3
		 ORDER BY date, id
	`, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.UserID, &e.Date, &e.Text, &e.RemindAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (c *Calendar) byID(ctx context.Context, id int64) (Event, error) {
	var e Event
	err := c.pool.QueryRow(ctx, `
		SELECT id, user_id, date, text, remind_at FROM events WHERE id = $1
	`, id).Scan(&e.ID, &e.UserID, &e.Date, &e.Text, &e.RemindAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrEventNotFound
	}
	return e, err
}

func truncateDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
