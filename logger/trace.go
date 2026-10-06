package logger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type traceKey struct{}

func StartTrace(ctx context.Context) context.Context {
	if TraceID(ctx) != "" {
		return ctx
	}
	return WithTraceID(ctx, NewTraceID())
}

func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(traceKey{}).(string)
	return id
}

func NewTraceID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
