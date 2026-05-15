CREATE TABLE IF NOT EXISTS events (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT      NOT NULL,
    date            DATE        NOT NULL,
    text            TEXT        NOT NULL,
    remind_at       TIMESTAMPTZ NULL,
    reminder_fired  BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_events_user_date ON events (user_id, date);

CREATE INDEX IF NOT EXISTS idx_events_pending_reminder
    ON events (remind_at)
    WHERE remind_at IS NOT NULL AND reminder_fired = FALSE;

CREATE TABLE IF NOT EXISTS archive_entries (
    id              BIGSERIAL PRIMARY KEY,
    original_event_id BIGINT    NOT NULL,
    user_id         BIGINT      NOT NULL,
    date            DATE        NOT NULL,
    text            TEXT        NOT NULL,
    remind_at       TIMESTAMPTZ NULL,
    archived_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_archive_user ON archive_entries (user_id);

CREATE TABLE IF NOT EXISTS fired_reminders (
    id          BIGSERIAL PRIMARY KEY,
    event_id    BIGINT      NOT NULL,
    user_id     BIGINT      NOT NULL,
    text        TEXT        NOT NULL,
    remind_at   TIMESTAMPTZ NOT NULL,
    fired_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_fired_user ON fired_reminders (user_id, fired_at DESC);
