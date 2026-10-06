package logger

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixedLogger(buf *bytes.Buffer, min Level) *Logger {
	l := New(buf, min)
	l.now = func() time.Time {
		return time.Date(2026, 10, 6, 16, 5, 8, 581_000_000, time.FixedZone("IST", 5*3600+1800))
	}
	return l
}

func TestFormat(t *testing.T) {
	var buf bytes.Buffer
	l := fixedLogger(&buf, LevelDebug)
	ctx := WithTraceID(context.Background(), "abc123")
	l.Info(ctx, "tag_k3f9a2", "tuple written", "object", "document:1", "note", "two words")
	want := `2026-10-06T16:05:08.581+05:30 INFO  tag=tag_k3f9a2 pkg=logger trace=abc123 msg="tuple written" object=document:1 note="two words"` + "\n"
	if buf.String() != want {
		t.Fatalf("got  %q\nwant %q", buf.String(), want)
	}
}

func TestMissingTraceAndTag(t *testing.T) {
	var buf bytes.Buffer
	fixedLogger(&buf, LevelDebug).Error(context.Background(), "", "boom", "odd")
	want := "2026-10-06T16:05:08.581+05:30 ERROR tag=- pkg=logger trace=- msg=boom !BADKEY=odd\n"
	if buf.String() != want {
		t.Fatalf("got  %q\nwant %q", buf.String(), want)
	}
}

func TestMinLevel(t *testing.T) {
	var buf bytes.Buffer
	l := fixedLogger(&buf, LevelWarn)
	ctx := context.Background()
	l.Debug(ctx, "aaaaaa", "d")
	l.Info(ctx, "bbbbbb", "i")
	l.Warn(ctx, "cccccc", "w")
	l.Error(ctx, "dddddd", "e")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], " WARN ") || !strings.Contains(lines[1], " ERROR ") {
		t.Fatalf("lines = %q", lines)
	}
}

func TestNilLoggerIsNoop(t *testing.T) {
	var l *Logger
	l.Info(context.Background(), "ffffff", "ignored")
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]Level{"debug": LevelDebug, "INFO": LevelInfo, "Warn": LevelWarn, "error": LevelError} {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseLevel("verbose"); err == nil {
		t.Error("ParseLevel(verbose) succeeded")
	}
	var l Level
	if err := l.UnmarshalText([]byte("warn")); err != nil || l != LevelWarn {
		t.Errorf("UnmarshalText = %v, %v", l, err)
	}
}

func TestLevelString(t *testing.T) {
	for l, want := range map[Level]string{LevelDebug: "DEBUG", LevelInfo: "INFO", LevelWarn: "WARN", LevelError: "ERROR", Level(9): "LEVEL(9)"} {
		if l.String() != want {
			t.Errorf("%d.String() = %q, want %q", l, l.String(), want)
		}
	}
}

func TestStartTrace(t *testing.T) {
	ctx := StartTrace(context.Background())
	id := TraceID(ctx)
	if len(id) != 32 {
		t.Fatalf("trace id = %q", id)
	}
	if got := TraceID(StartTrace(ctx)); got != id {
		t.Fatalf("StartTrace replaced existing id: %q -> %q", id, got)
	}
	if NewTraceID() == NewTraceID() {
		t.Fatal("trace ids not unique")
	}
}

func TestConcurrentLinesIntact(t *testing.T) {
	var buf bytes.Buffer
	l := fixedLogger(&buf, LevelDebug)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := StartTrace(context.Background())
			for range 100 {
				l.Info(ctx, "eeeeee", "concurrent")
			}
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 800 {
		t.Fatalf("lines = %d, want 800", len(lines))
	}
	for _, line := range lines {
		if !strings.HasSuffix(line, "msg=concurrent") {
			t.Fatalf("corrupted line %q", line)
		}
	}
}
