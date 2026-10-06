package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/c4ptlevi/margit/cache"
	"github.com/c4ptlevi/margit/model"
	"github.com/c4ptlevi/margit/store"
)

type countingEngine struct {
	ReBACEngine
	checks, expands, lookups int
	allowed                  bool
}

func (c *countingEngine) Check(context.Context, model.Entity, string, model.Entity) (bool, error) {
	c.checks++
	return c.allowed, nil
}

func (c *countingEngine) Expand(context.Context, model.Entity, string) ([]model.Entity, error) {
	c.expands++
	return []model.Entity{ent("user:a")}, nil
}

func (c *countingEngine) Lookup(_ context.Context, _ model.Entity, _ string, _ string, p Page) ([]model.Entity, string, error) {
	c.lookups++
	return []model.Entity{ent("doc:" + p.After)}, "next", nil
}

func (c *countingEngine) SaveNamespace(context.Context, model.Namespace) error { return nil }

func newCachedForTest(inner ReBACEngine, ttl time.Duration, maxEntries int) (*CachedEngine, *time.Time) {
	mem := NewMemResponseCache(context.Background(), CacheConfig{TTLMillis: int(ttl / time.Millisecond), MaxEntries: maxEntries}, nil)
	now := time.Unix(1000, 0)
	mem.Now = func() time.Time { return now }
	return NewCached(inner, mem, nil), &now
}

func TestCachedCheckTTL(t *testing.T) {
	ctx := context.Background()
	inner := &countingEngine{allowed: true}
	c, now := newCachedForTest(inner, time.Second, 100)
	for range 3 {
		if ok, err := c.Check(ctx, ent("doc:1"), "viewer", ent("user:a")); err != nil || !ok {
			t.Fatalf("got %v %v", ok, err)
		}
	}
	if inner.checks != 1 {
		t.Fatalf("inner checks = %d, want 1", inner.checks)
	}
	inner.allowed = false
	*now = now.Add(999 * time.Millisecond)
	if ok, _ := c.Check(ctx, ent("doc:1"), "viewer", ent("user:a")); !ok {
		t.Fatal("expected cached true before ttl")
	}
	*now = now.Add(time.Millisecond)
	if ok, _ := c.Check(ctx, ent("doc:1"), "viewer", ent("user:a")); ok {
		t.Fatal("expected fresh false after ttl")
	}
	if s := c.CacheStats(); s.Hits != 3 || s.Misses != 2 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestCachedFullConsistencyBypasses(t *testing.T) {
	inner := &countingEngine{allowed: true}
	c, _ := newCachedForTest(inner, time.Minute, 100)
	full := WithConsistency(context.Background(), FullyConsistent)
	c.Check(full, ent("doc:1"), "viewer", ent("user:a"))
	c.Check(full, ent("doc:1"), "viewer", ent("user:a"))
	if inner.checks != 2 {
		t.Fatalf("inner checks = %d, want 2", inner.checks)
	}
	c.Check(context.Background(), ent("doc:1"), "viewer", ent("user:a"))
	if inner.checks != 2 {
		t.Fatal("full read should refresh the cache")
	}
	if s := c.CacheStats(); s.Bypasses != 2 || s.Hits != 1 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestCachedKeysAndNamespaceClear(t *testing.T) {
	ctx := context.Background()
	inner := &countingEngine{}
	c, _ := newCachedForTest(inner, time.Minute, 100)
	c.Check(ctx, ent("doc:1"), "viewer", ent("user:a"))
	c.Check(ctx, ent("doc:1"), "viewer", ent("user:b"))
	c.Check(ctx, ent("doc:1"), "editor", ent("user:a"))
	c.Expand(ctx, ent("doc:1"), "viewer")
	c.Expand(ctx, ent("doc:1"), "viewer")
	c.Lookup(ctx, ent("user:a"), "viewer", "doc", Page{After: "1", Limit: 10})
	objs, next, _ := c.Lookup(ctx, ent("user:a"), "viewer", "doc", Page{After: "1", Limit: 10})
	c.Lookup(ctx, ent("user:a"), "viewer", "doc", Page{After: "2", Limit: 10})
	if inner.checks != 3 || inner.expands != 1 || inner.lookups != 2 || next != "next" || objs[0] != ent("doc:1") {
		t.Fatalf("checks=%d expands=%d lookups=%d next=%q objs=%v", inner.checks, inner.expands, inner.lookups, next, objs)
	}
	if err := c.SaveNamespace(ctx, ns("doc")); err != nil {
		t.Fatal(err)
	}
	if n := c.CacheStats().Entries; n != 0 {
		t.Fatalf("entries after namespace save = %d", n)
	}
}

func TestTTLCacheBounded(t *testing.T) {
	ctx := context.Background()
	var c ResponseCache = NewMemResponseCache(ctx, CacheConfig{TTLMillis: 60_000, MaxEntries: cache.Shards * 8}, nil)
	for i := range 10_000 {
		c.Set(ctx, fmt.Sprint(i), i)
	}
	if n := c.Len(ctx); n > cache.Shards*8 {
		t.Fatalf("len = %d, want <= %d", n, cache.Shards*8)
	}
	c.Set(ctx, "k", 1)
	if v, ok := c.Get(ctx, "k"); !ok || v != 1 {
		t.Fatalf("get = %v %v", v, ok)
	}
}

func TestCachedEngineIntegration(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, Config{}, nil)
	c := NewCached(e, NewMemResponseCache(ctx, CacheConfig{TTLMillis: 60_000}, nil), nil)
	if ok, err := c.Check(ctx, ent("folder:root"), "viewer", ent("user:zed")); err != nil || ok {
		t.Fatalf("got %v %v", ok, err)
	}
	if err := c.WriteTuples(ctx, []model.RelationTuple{tuple("folder:root", "viewer_direct", "user:zed")}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Check(ctx, ent("folder:root"), "viewer", ent("user:zed")); ok {
		t.Fatal("expected stale cached false within ttl")
	}
	if ok, _ := c.Check(WithConsistency(ctx, FullyConsistent), ent("folder:root"), "viewer", ent("user:zed")); !ok {
		t.Fatal("expected fresh true with full consistency")
	}
	if _, err := c.Check(ctx, ent("folder:root"), "nope", ent("user:zed")); err == nil {
		t.Fatal("expected error for unknown relation")
	}
}

func TestQueryCacheFreshAfterLocalWrite(t *testing.T) {
	ctx := context.Background()
	base := newStore
	var qs *store.CachedStore
	newStore = func(t *testing.T) store.Store {
		qs = store.NewCached(base(t), cache.NewMem(ctx, "query", CacheConfig{TTLMillis: 60_000}, nil), nil)
		return qs
	}
	defer func() { newStore = base }()
	e := newTestEngine(t, Config{}, nil)
	if ok, _ := e.Check(ctx, ent("folder:root"), "viewer", ent("user:zed")); ok {
		t.Fatal("zed should not view root yet")
	}
	if ok, _ := e.Check(ctx, ent("folder:root"), "viewer", ent("user:zed")); ok {
		t.Fatal("zed should not view root yet")
	}
	if qs.CacheStats().Hits == 0 {
		t.Fatal("expected query cache hits on repeated check")
	}
	if err := e.WriteTuples(ctx, []model.RelationTuple{tuple("folder:root", "viewer_direct", "user:zed")}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := e.Check(ctx, ent("folder:root"), "viewer", ent("user:zed")); !ok {
		t.Fatal("write must invalidate the cached tuple read")
	}
}
