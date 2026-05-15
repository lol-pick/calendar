package logger

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

type Level string

const (
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

type entry struct {
	ts    time.Time
	level Level
	msg   string
}

type Logger struct {
	ch      chan entry
	out     io.Writer
	done    chan struct{}
	closed  atomic.Bool
	wg      sync.WaitGroup
	prefix  string
	dropped atomic.Int64
}

func New(out io.Writer, prefix string, bufferSize int) *Logger {
	if bufferSize <= 0 {
		bufferSize = 1
	}
	l := &Logger{
		ch:     make(chan entry, bufferSize),
		out:    out,
		done:   make(chan struct{}),
		prefix: prefix,
	}
	l.wg.Add(1)
	go l.run()
	return l
}

func (l *Logger) run() {
	defer l.wg.Done()
	defer close(l.done)
	for e := range l.ch {
		line := fmt.Sprintf("%s %s %s %s\n",
			e.ts.UTC().Format(time.RFC3339),
			l.prefix,
			e.level,
			e.msg,
		)
		_, _ = io.WriteString(l.out, line)
	}
}

func (l *Logger) log(level Level, format string, args ...any) {
	if l.closed.Load() {
		return
	}
	e := entry{
		ts:    time.Now(),
		level: level,
		msg:   fmt.Sprintf(format, args...),
	}
	select {
	case l.ch <- e:
	default:
		l.dropped.Add(1)
	}
}

func (l *Logger) Infof(format string, args ...any) { l.log(LevelInfo, format, args...) }

func (l *Logger) Warnf(format string, args ...any) { l.log(LevelWarn, format, args...) }

func (l *Logger) Errorf(format string, args ...any) { l.log(LevelError, format, args...) }

func (l *Logger) Dropped() int64 { return l.dropped.Load() }

func (l *Logger) Close() {
	if !l.closed.CompareAndSwap(false, true) {
		return
	}
	close(l.ch)
	l.wg.Wait()
}
