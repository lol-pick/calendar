package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL string

	Addr string

	CleanupInterval time.Duration

	ArchiveAfter time.Duration

	LogBuffer int

	ReminderBuffer int

	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:     envOr("DATABASE_URL", ""),
		Addr:            envOr("ADDR", ":8080"),
		CleanupInterval: envDuration("CLEANUP_INTERVAL", 10*time.Minute),
		ArchiveAfter:    envDuration("ARCHIVE_AFTER", 24*time.Hour),
		LogBuffer:       envInt("LOG_BUFFER", 256),
		ReminderBuffer:  envInt("REMINDER_BUFFER", 128),
		ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 5*time.Second),
	}

	dbURL := flag.String("database-url", cfg.DatabaseURL, "Postgres DSN (or env DATABASE_URL)")
	addr := flag.String("addr", cfg.Addr, "HTTP listen address (e.g. :8080)")
	cleanup := flag.Duration("cleanup-interval", cfg.CleanupInterval, "how often cleanup goroutine runs (e.g. 10m)")
	archive := flag.Duration("archive-after", cfg.ArchiveAfter, "events older than this are archived (e.g. 24h)")
	logBuf := flag.Int("log-buffer", cfg.LogBuffer, "async logger channel buffer size")
	remBuf := flag.Int("reminder-buffer", cfg.ReminderBuffer, "reminder worker channel buffer size")
	shutTO := flag.Duration("shutdown-timeout", cfg.ShutdownTimeout, "graceful shutdown timeout")

	flag.Parse()

	cfg.DatabaseURL = *dbURL
	cfg.Addr = *addr
	cfg.CleanupInterval = *cleanup
	cfg.ArchiveAfter = *archive
	cfg.LogBuffer = *logBuf
	cfg.ReminderBuffer = *remBuf
	cfg.ShutdownTimeout = *shutTO

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: DATABASE_URL is required (env or --database-url)")
	}
	if c.Addr == "" {
		return fmt.Errorf("config: empty Addr")
	}
	if c.CleanupInterval <= 0 {
		return fmt.Errorf("config: CleanupInterval must be > 0")
	}
	if c.ArchiveAfter <= 0 {
		return fmt.Errorf("config: ArchiveAfter must be > 0")
	}
	if c.LogBuffer <= 0 {
		return fmt.Errorf("config: LogBuffer must be > 0")
	}
	if c.ReminderBuffer <= 0 {
		return fmt.Errorf("config: ReminderBuffer must be > 0")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("config: ShutdownTimeout must be > 0")
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
