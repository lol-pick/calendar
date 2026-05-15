package cleanup

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeMover struct {
	mu     sync.Mutex
	calls  int
	cutoff time.Time
	ret    int
	err    error
}

func (m *fakeMover) MoveOlderToArchive(_ context.Context, cutoff time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.cutoff = cutoff
	return m.ret, m.err
}

type noopLog struct{}

func (noopLog) Infof(string, ...any)  {}
func (noopLog) Errorf(string, ...any) {}

func TestRunner_RunOnce_CallsMoverWithCutoff(t *testing.T) {
	m := &fakeMover{ret: 5}
	r := New(m, noopLog{}, time.Minute, 24*time.Hour)
	r.now = func() time.Time { return time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC) }

	r.runOnce(context.Background())

	if m.calls != 1 {
		t.Fatalf("expected 1 call, got %d", m.calls)
	}
	want := time.Date(2025, 5, 31, 12, 0, 0, 0, time.UTC)
	if !m.cutoff.Equal(want) {
		t.Errorf("expected cutoff=%v, got %v", want, m.cutoff)
	}
}

func TestRunner_RunsOnTicker(t *testing.T) {
	m := &fakeMover{}
	r := New(m, noopLog{}, 20*time.Millisecond, 24*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	time.Sleep(80 * time.Millisecond)
	cancel()
	<-r.Done()

	m.mu.Lock()
	calls := m.calls
	m.mu.Unlock()
	if calls < 2 {
		t.Errorf("expected at least 2 cleanup runs, got %d", calls)
	}
}
