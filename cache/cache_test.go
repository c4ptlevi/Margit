package cache

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestMemTTLAndDelete(t *testing.T) {
	ctx := context.Background()
	c := NewMem(ctx, "t", Config{TTLMillis: 1000}, nil)
	now := time.Unix(1000, 0)
	c.Now = func() time.Time { return now }
	c.Set(ctx, "k", 1)
	now = now.Add(999 * time.Millisecond)
	if v, ok := c.Get(ctx, "k"); !ok || v != 1 {
		t.Fatalf("before ttl: %v %v", v, ok)
	}
	now = now.Add(time.Millisecond)
	if _, ok := c.Get(ctx, "k"); ok {
		t.Fatal("expired entry returned")
	}
	c.Set(ctx, "k", 2)
	c.Delete(ctx, "k")
	if _, ok := c.Get(ctx, "k"); ok {
		t.Fatal("deleted entry returned")
	}
}

func TestMemBounded(t *testing.T) {
	ctx := context.Background()
	c := NewMem(ctx, "t", Config{TTLMillis: 60_000, MaxEntries: Shards * 8}, nil)
	for i := range 10_000 {
		c.Set(ctx, strconv.Itoa(i), i)
	}
	if n := c.Len(ctx); n > Shards*8 {
		t.Fatalf("len = %d, want <= %d", n, Shards*8)
	}
	c.Clear(ctx)
	if n := c.Len(ctx); n != 0 {
		t.Fatalf("len after clear = %d", n)
	}
}

func TestBypassAndCounters(t *testing.T) {
	if Bypassed(context.Background()) || !Bypassed(WithBypass(context.Background())) {
		t.Fatal("bypass flag")
	}
	var c Counters
	c.Hit()
	c.Hit()
	c.Miss()
	c.Bypass()
	if s := c.Stats(7); s != (Stats{Hits: 2, Misses: 1, Bypasses: 1, Entries: 7}) {
		t.Fatalf("stats = %+v", s)
	}
}
