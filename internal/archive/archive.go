package archive

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	ID              int64      `json:"id"`
	OriginalEventID int64      `json:"original_event_id"`
	UserID          int64      `json:"user_id"`
	Date            time.Time  `json:"date"`
	Text            string     `json:"text"`
	RemindAt        *time.Time `json:"remind_at,omitempty"`
	ArchivedAt      time.Time  `json:"archived_at"`
}

type Archive struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Archive {
	return &Archive{pool: pool}
}

func (a *Archive) List(ctx context.Context, userID int64) ([]Entry, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT id, original_event_id, user_id, date, text, remind_at, archived_at
		  FROM archive_entries
		 WHERE user_id = $1
		 ORDER BY date DESC, id DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query archive: %w", err)
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.OriginalEventID, &e.UserID,
			&e.Date, &e.Text, &e.RemindAt, &e.ArchivedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
