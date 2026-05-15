package logger

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestLogger_WritesEntries(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "[test]", 8)

	l.Infof("hello %s", "world")
	l.Warnf("careful")
	l.Errorf("boom")

	l.Close()

	out := buf.String()
	for _, want := range []string{"hello world", "careful", "boom", "INFO", "WARN", "ERROR", "[test]"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestLogger_DoesNotBlockOnFullChannel(t *testing.T) {
	slow := &blockingWriter{ch: make(chan struct{})}
	l := New(slow, "[t]", 1)

	for i := 0; i < 100; i++ {
		l.Infof("msg %d", i)
	}

	close(slow.ch)
	l.Close()

	if l.Dropped() == 0 {
		t.Errorf("expected some dropped entries, got 0")
	}
}

func TestLogger_CloseIsIdempotent(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "[t]", 1)
	l.Close()
	l.Close()
}

type blockingWriter struct {
	once sync.Once
	ch   chan struct{}
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { <-w.ch })
	return len(p), nil
}
