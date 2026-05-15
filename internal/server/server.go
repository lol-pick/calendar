package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"calendar/internal/archive"
	"calendar/internal/calendar"
	"calendar/internal/reminder"
)

type CalendarService interface {
	Create(ctx context.Context, userID int64, date time.Time, text string, remindAt *time.Time) (calendar.Event, error)
	Update(ctx context.Context, id, userID int64, date time.Time, text string) (calendar.Event, error)
	Delete(ctx context.Context, id, userID int64) error
	ForDay(ctx context.Context, userID int64, day time.Time) ([]calendar.Event, error)
	ForWeek(ctx context.Context, userID int64, day time.Time) ([]calendar.Event, error)
	ForMonth(ctx context.Context, userID int64, day time.Time) ([]calendar.Event, error)
}

type ReminderScheduler interface {
	Schedule(e calendar.Event) bool
	Fired(ctx context.Context, userID int64) ([]reminder.Fired, error)
}

type ArchiveReader interface {
	List(ctx context.Context, userID int64) ([]archive.Entry, error)
}

// Logger — асинхронный логгер.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

type Server struct {
	cal      CalendarService
	reminder ReminderScheduler
	archive  ArchiveReader
	logger   Logger
}

func New(cal CalendarService, rem ReminderScheduler, arc ArchiveReader, logger Logger) *Server {
	return &Server{cal: cal, reminder: rem, archive: arc, logger: logger}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /create_event", s.createEvent)
	mux.HandleFunc("POST /update_event", s.updateEvent)
	mux.HandleFunc("POST /delete_event", s.deleteEvent)

	mux.HandleFunc("GET /events_for_day", s.eventsForDay)
	mux.HandleFunc("GET /events_for_week", s.eventsForWeek)
	mux.HandleFunc("GET /events_for_month", s.eventsForMonth)

	mux.HandleFunc("GET /reminders", s.reminders)
	mux.HandleFunc("GET /archive", s.archiveList)

	return LoggingMiddleware(s.logger)(mux)
}

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	userID, date, text, remindAt, err := parseEventForm(r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	e, err := s.cal.Create(r.Context(), userID, date, text, remindAt)
	if err != nil {
		writeBusinessError(w, err)
		return
	}

	if remindAt != nil {
		if ok := s.reminder.Schedule(e); !ok {
			s.logger.Warnf("reminder channel full, event_id=%d will be picked up on next start", e.ID)
		}
	}

	writeResult(w, e)
}

func (s *Server) updateEvent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "cannot parse form")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	userID, date, text, _, err := parseEventForm(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := s.cal.Update(r.Context(), id, userID, date, text)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	writeResult(w, e)
}

func (s *Server) deleteEvent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "cannot parse form")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	userID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	if err := s.cal.Delete(r.Context(), id, userID); err != nil {
		writeBusinessError(w, err)
		return
	}
	writeResult(w, "deleted")
}

func (s *Server) eventsForDay(w http.ResponseWriter, r *http.Request) {
	s.eventsRange(w, r, s.cal.ForDay)
}
func (s *Server) eventsForWeek(w http.ResponseWriter, r *http.Request) {
	s.eventsRange(w, r, s.cal.ForWeek)
}
func (s *Server) eventsForMonth(w http.ResponseWriter, r *http.Request) {
	s.eventsRange(w, r, s.cal.ForMonth)
}

func (s *Server) eventsRange(
	w http.ResponseWriter,
	r *http.Request,
	method func(context.Context, int64, time.Time) ([]calendar.Event, error),
) {
	userID, err := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	date, err := time.Parse(calendar.DateLayout, r.URL.Query().Get("date"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date (expected YYYY-MM-DD)")
		return
	}

	events, err := method(r.Context(), userID, date)
	if err != nil {
		s.logger.Errorf("range query: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if events == nil {
		events = []calendar.Event{}
	}
	writeResult(w, events)
}

func (s *Server) reminders(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	fired, err := s.reminder.Fired(r.Context(), userID)
	if err != nil {
		s.logger.Errorf("fired: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if fired == nil {
		fired = []reminder.Fired{}
	}
	writeResult(w, fired)
}

func (s *Server) archiveList(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	entries, err := s.archive.List(r.Context(), userID)
	if err != nil {
		s.logger.Errorf("archive list: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if entries == nil {
		entries = []archive.Entry{}
	}
	writeResult(w, entries)
}

func parseEventForm(r *http.Request, allowRemindAt bool) (
	userID int64, date time.Time, text string, remindAt *time.Time, err error,
) {
	if err = r.ParseForm(); err != nil {
		return 0, time.Time{}, "", nil, errors.New("cannot parse form")
	}
	userID, err = strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		return 0, time.Time{}, "", nil, errors.New("invalid user_id")
	}
	date, err = time.Parse(calendar.DateLayout, r.FormValue("date"))
	if err != nil {
		return 0, time.Time{}, "", nil, errors.New("invalid date (expected YYYY-MM-DD)")
	}
	text = r.FormValue("event")
	if text == "" {
		return 0, time.Time{}, "", nil, errors.New("event text is required")
	}
	if allowRemindAt {
		if raw := r.FormValue("remind_at"); raw != "" {
			t, perr := time.Parse(time.RFC3339, raw)
			if perr != nil {
				return 0, time.Time{}, "", nil, errors.New("invalid remind_at (expected RFC3339)")
			}
			remindAt = &t
		}
	}
	return userID, date, text, remindAt, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeResult(w http.ResponseWriter, result any) {
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeBusinessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, calendar.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, calendar.ErrEventNotFound):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
