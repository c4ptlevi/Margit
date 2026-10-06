package store

import (
	"context"
	"testing"
)

func TestOpen(t *testing.T) {
	ctx := context.Background()
	s, closeFn, err := Open(ctx, Config{Type: KindMemory}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	if _, ok := s.(*MemoryStore); !ok {
		t.Fatalf("Open(memory) = %T", s)
	}
	if _, _, err := Open(ctx, Config{Type: "redis"}, nil); err == nil {
		t.Fatal("Open(redis) succeeded")
	}
}
