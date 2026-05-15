package cleanup

import (
	"context"
	"time"
)

type Mover interface {
	MoveOlderToArchive(ctx context.Context, cutoff time.Time) (int, error)
}

type Logger interface {
	Infof(format string, args ...any)
	Errorf(format string, args ...any)
}

type Runner struct {
	mover        Mover
	logger       Logger
	interval     time.Duration
	archiveAfter time.Duration
	now          func() time.Time
	done         chan struct{}
}

func New(mover Mover, logger Logger, interval, archiveAfter time.Duration) *Runner {
	return &Runner{
		mover:        mover,
		logger:       logger,
		interval:     interval,
		archiveAfter: archiveAfter,
		now:          time.Now,
		done:         make(chan struct{}),
	}
}

func (r *Runner) Done() <-chan struct{} { return r.done }

func (r *Runner) Run(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.runOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	cutoff := r.now().Add(-r.archiveAfter)
	moved, err := r.mover.MoveOlderToArchive(ctx, cutoff)
	if err != nil {
		r.logger.Errorf("cleanup: move failed: %v", err)
		return
	}
	if moved > 0 {
		r.logger.Infof("cleanup: archived %d event(s) older than %s",
			moved, cutoff.Format(time.RFC3339))
	}
}
