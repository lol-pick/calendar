package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"calendar/internal/archive"
	"calendar/internal/calendar"
	"calendar/internal/cleanup"
	"calendar/internal/config"
	"calendar/internal/logger"
	"calendar/internal/reminder"
	"calendar/internal/server"
	"calendar/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logr := logger.New(os.Stdout, "[calendar]", cfg.LogBuffer)
	logr.Infof("starting service on %s", cfg.Addr)
	logr.Infof("cleanup every %s, archive after %s", cfg.CleanupInterval, cfg.ArchiveAfter)

	bootCtx, bootCancel := context.WithCancel(context.Background())
	pool, err := storage.Open(bootCtx, cfg.DatabaseURL)
	if err != nil {
		logr.Errorf("open postgres: %v", err)
		logr.Close()
		bootCancel()
		os.Exit(1)
	}
	if err := storage.Migrate(bootCtx, pool); err != nil {
		logr.Errorf("migrate: %v", err)
		pool.Close()
		logr.Close()
		bootCancel()
		os.Exit(1)
	}
	bootCancel()
	logr.Infof("postgres connected and migrated")

	cal := calendar.New(pool)
	arc := archive.New(pool)

	ctx, cancel := context.WithCancel(context.Background())

	rem := reminder.New(pool, logr, cfg.ReminderBuffer)
	go rem.Run(ctx)

	clean := cleanup.New(cal, logr, cfg.CleanupInterval, cfg.ArchiveAfter)
	go clean.Run(ctx)

	srv := server.New(cal, rem, arc, logr)
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: cfg.ShutdownTimeout,
	}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logr.Errorf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logr.Infof("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logr.Errorf("http shutdown error: %v", err)
	}

	cancel()
	<-rem.Done()
	<-clean.Done()

	pool.Close()

	logr.Infof("bye (dropped log entries: %d)", logr.Dropped())
	logr.Close()
}
