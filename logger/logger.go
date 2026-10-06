package logger

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Level int8

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelText = [...]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

func (l Level) String() string {
	if l < LevelDebug || l > LevelError {
		return "LEVEL(" + strconv.Itoa(int(l)) + ")"
	}
	return levelText[l]
}

func ParseLevel(s string) (Level, error) {
	for l, text := range levelText {
		if strings.EqualFold(s, text) {
			return Level(l), nil
		}
	}
	return 0, fmt.Errorf("unknown log level %q", s)
}

func (l Level) MarshalText() ([]byte, error) {
	return []byte(l.String()), nil
}

func (l *Level) UnmarshalText(b []byte) error {
	parsed, err := ParseLevel(string(b))
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

type Logger struct {
	mu  sync.Mutex
	w   io.Writer
	min Level
	now func() time.Time
}

func New(w io.Writer, min Level) *Logger {
	return &Logger{w: w, min: min, now: time.Now}
}

func (l *Logger) Debug(ctx context.Context, tag, msg string, kv ...any) {
	l.log(ctx, LevelDebug, tag, msg, kv)
}

func (l *Logger) Info(ctx context.Context, tag, msg string, kv ...any) {
	l.log(ctx, LevelInfo, tag, msg, kv)
}

func (l *Logger) Warn(ctx context.Context, tag, msg string, kv ...any) {
	l.log(ctx, LevelWarn, tag, msg, kv)
}

func (l *Logger) Error(ctx context.Context, tag, msg string, kv ...any) {
	l.log(ctx, LevelError, tag, msg, kv)
}

func (l *Logger) log(ctx context.Context, level Level, tag, msg string, kv []any) {
	if l == nil || level < l.min {
		return
	}
	trace := TraceID(ctx)
	if trace == "" {
		trace = "-"
	}
	if tag == "" {
		tag = "-"
	}

	var b strings.Builder
	b.WriteString(l.now().Format(TimeFormat))
	b.WriteByte(' ')
	fmt.Fprintf(&b, "%-5s", level)
	b.WriteString(" tag=")
	b.WriteString(tag)
	b.WriteString(" pkg=")
	b.WriteString(callerPackage(3))
	b.WriteString(" trace=")
	b.WriteString(trace)
	b.WriteString(" msg=")
	b.WriteString(quote(msg))
	for i := 0; i < len(kv); i += 2 {
		key, val := "!BADKEY", kv[i]
		if i+1 < len(kv) {
			key, val = fmt.Sprint(kv[i]), kv[i+1]
		}
		b.WriteByte(' ')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(quote(fmt.Sprint(val)))
	}
	b.WriteByte('\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	io.WriteString(l.w, b.String())
}

func callerPackage(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "-"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "-"
	}
	name := fn.Name()
	name = name[strings.LastIndexByte(name, '/')+1:]
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	return name
}

func quote(s string) string {
	if s == "" || strings.ContainsAny(s, " =\"\t\r\n") {
		return strconv.Quote(s)
	}
	return s
}
