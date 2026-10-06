package store

import (
	"context"
	"testing"
	"time"

	"github.com/c4ptlevi/margit/cache"
	"github.com/c4ptlevi/margit/model"
)

type countingStore struct {
	Store
	reads, bySubject, namespaces int
}

func (c *countingStore) ReadTuples(ctx context.Context, obj model.Entity, rel string) ([]model.RelationTuple, error) {
	c.reads++
	return c.Store.ReadTuples(ctx, obj, rel)
}

func (c *countingStore) ReadTuplesBySubject(ctx context.Context, sub model.Entity, rel string) ([]model.RelationTuple, error) {
	c.bySubject++
	return c.Store.ReadTuplesBySubject(ctx, sub, rel)
}

func (c *countingStore) GetNamespace(ctx context.Context, name string) (model.Namespace, error) {
	c.namespaces++
	return c.Store.GetNamespace(ctx, name)
}

func newCachedTestStore(t *testing.T) (*CachedStore, *countingStore, *time.Time) {
	t.Helper()
	ctx := context.Background()
	inner := &countingStore{Store: NewMemoryStore(nil)}
	mem := cache.NewMem(ctx, "query", cache.Config{TTLMillis: 1000}, nil)
	now := time.Unix(1000, 0)
	mem.Now = func() time.Time { return now }
	s := NewCached(inner, mem, nil)
	if err := s.SaveNamespace(ctx, model.Namespace{Name: "doc", Relations: map[string]model.Relation{"viewer": {Name: "viewer", AllowedTypes: []string{"user"}}}}); err != nil {
		t.Fatal(err)
	}
	return s, inner, &now
}

func TestCachedStoreReadsAndInvalidation(t *testing.T) {
	ctx := context.Background()
	s, inner, _ := newCachedTestStore(t)
	doc, alice, bob := model.Entity{Namespace: "doc", ID: "1"}, model.Entity{Namespace: "user", ID: "alice"}, model.Entity{Namespace: "user", ID: "bob"}
	s.WriteTuples(ctx, []model.RelationTuple{{Object: doc, Relation: "viewer", Subject: alice}})
	for range 3 {
		if ts, _ := s.ReadTuples(ctx, doc, "viewer"); len(ts) != 1 {
			t.Fatalf("tuples = %v", ts)
		}
		s.ReadTuplesBySubject(ctx, alice, "viewer")
	}
	if inner.reads != 1 || inner.bySubject != 1 {
		t.Fatalf("reads=%d bySubject=%d, want 1/1", inner.reads, inner.bySubject)
	}
	s.WriteTuples(ctx, []model.RelationTuple{{Object: doc, Relation: "viewer", Subject: bob}})
	if ts, _ := s.ReadTuples(ctx, doc, "viewer"); len(ts) != 2 {
		t.Fatalf("after write tuples = %v", ts)
	}
	s.DeleteTuples(ctx, []model.RelationTuple{{Object: doc, Relation: "viewer", Subject: alice}})
	if ts, _ := s.ReadTuplesBySubject(ctx, alice, "viewer"); len(ts) != 0 {
		t.Fatalf("after delete by subject = %v", ts)
	}
	if st := s.CacheStats(); st.Hits != 4 || st.Misses != 4 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestCachedStoreTTLBypassAndNamespaces(t *testing.T) {
	ctx := context.Background()
	s, inner, now := newCachedTestStore(t)
	s.GetNamespace(ctx, "doc")
	s.GetNamespace(ctx, "doc")
	if inner.namespaces != 1 {
		t.Fatalf("namespace reads = %d", inner.namespaces)
	}
	*now = now.Add(time.Second)
	s.GetNamespace(ctx, "doc")
	if inner.namespaces != 2 {
		t.Fatal("expected reload after ttl")
	}
	s.GetNamespace(cache.WithBypass(ctx), "doc")
	if inner.namespaces != 3 || s.CacheStats().Bypasses != 1 {
		t.Fatal("bypass should read the store")
	}
	if _, err := s.GetNamespace(ctx, "missing"); err == nil {
		t.Fatal("expected not found")
	}
	if _, err := s.GetNamespace(ctx, "missing"); err == nil || inner.namespaces != 5 {
		t.Fatalf("errors must not be cached: reads=%d", inner.namespaces)
	}
	if err := s.DeleteNamespace(ctx, "doc"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNamespace(ctx, "doc"); err == nil {
		t.Fatal("deleted namespace still cached")
	}
}
