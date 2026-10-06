package engine

import (
	"context"
	"hash/maphash"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
)

const (
	DefaultCacheMaxEntries = 1_000_000
	cacheShards            = 64
)

type CacheConfig struct {
	TTLMillis  int `json:"ttl_ms"`
	MaxEntries int `json:"max_entries"`
}

type Consistency string

const (
	MinimizeLatency Consistency = "minimize_latency"
	FullyConsistent Consistency = "full"
)

type consistencyKey struct{}

func WithConsistency(ctx context.Context, c Consistency) context.Context {
	return context.WithValue(ctx, consistencyKey{}, c)
}

func consistencyFrom(ctx context.Context) Consistency {
	c, _ := ctx.Value(consistencyKey{}).(Consistency)
	return c
}

type CacheStats struct {
	Hits, Misses, Bypasses uint64
	Entries                int
}

type CacheStatser interface {
	CacheStats() CacheStats
}

type ResponseCache interface {
	Get(ctx context.Context, key string) (any, bool)
	Set(ctx context.Context, key string, v any)
	Clear(ctx context.Context)
	Len(ctx context.Context) int
}

var _ ResponseCache = (*MemResponseCache)(nil)

type entry struct {
	v   any
	exp int64
}

type shard struct {
	mu sync.Mutex
	m  map[string]entry
}

type MemResponseCache struct {
	seed     maphash.Seed
	ttl      time.Duration
	perShard int
	shards   [cacheShards]shard
	now      func() time.Time
	log      *logger.Logger
}

func NewMemResponseCache(ctx context.Context, cfg CacheConfig, log *logger.Logger) *MemResponseCache {
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = DefaultCacheMaxEntries
	}
	c := &MemResponseCache{
		seed:     maphash.MakeSeed(),
		ttl:      time.Duration(cfg.TTLMillis) * time.Millisecond,
		perShard: max(1, cfg.MaxEntries/cacheShards),
		now:      time.Now,
		log:      log,
	}
	log.Info(ctx, "tag_a4wmrd", "response cache created", "ttl", c.ttl, "max_entries", cfg.MaxEntries, "shards", cacheShards)
	for i := range c.shards {
		c.shards[i].m = make(map[string]entry)
	}
	return c
}

func (c *MemResponseCache) shard(key string) *shard {
	return &c.shards[maphash.String(c.seed, key)%cacheShards]
}

func (c *MemResponseCache) Get(_ context.Context, key string) (any, bool) {
	s := c.shard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok {
		return nil, false
	}
	if c.now().UnixNano() >= e.exp {
		delete(s.m, key)
		return nil, false
	}
	return e.v, true
}

func (c *MemResponseCache) Set(ctx context.Context, key string, v any) {
	s := c.shard(key)
	now := c.now().UnixNano()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; !ok && len(s.m) >= c.perShard {
		before := len(s.m)
		for k, e := range s.m {
			if now >= e.exp {
				delete(s.m, k)
			}
		}
		for k := range s.m {
			if len(s.m) < c.perShard-c.perShard/8 {
				break
			}
			delete(s.m, k)
		}
		c.log.Debug(ctx, "tag_emsjic", "response cache shard evicted", "before", before, "after", len(s.m), "limit", c.perShard)
	}
	s.m[key] = entry{v: v, exp: now + int64(c.ttl)}
}

func (c *MemResponseCache) Clear(ctx context.Context) {
	n := 0
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		n += len(s.m)
		s.m = make(map[string]entry)
		s.mu.Unlock()
	}
	c.log.Info(ctx, "tag_wrheg0", "response cache cleared", "entries", n)
}

func (c *MemResponseCache) Len(context.Context) int {
	n := 0
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		n += len(s.m)
		s.mu.Unlock()
	}
	return n
}

type lookupResult struct {
	objects []model.Entity
	next    string
}

var (
	_ ReBACEngine  = (*CachedEngine)(nil)
	_ CacheStatser = (*CachedEngine)(nil)
)

type CachedEngine struct {
	ReBACEngine
	cache                  ResponseCache
	hits, misses, bypasses atomic.Uint64
	log                    *logger.Logger
}

func NewCached(inner ReBACEngine, cache ResponseCache, log *logger.Logger) *CachedEngine {
	return &CachedEngine{ReBACEngine: inner, cache: cache, log: log}
}

func (c *CachedEngine) CacheStats() CacheStats {
	return CacheStats{
		Hits:     c.hits.Load(),
		Misses:   c.misses.Load(),
		Bypasses: c.bypasses.Load(),
		Entries:  c.cache.Len(context.Background()),
	}
}

func (c *CachedEngine) lookupCache(ctx context.Context, key string) (any, bool) {
	if consistencyFrom(ctx) == FullyConsistent {
		c.bypasses.Add(1)
		c.log.Debug(ctx, "tag_1odurh", "response cache bypassed", "key", printableKey(key))
		return nil, false
	}
	if v, ok := c.cache.Get(ctx, key); ok {
		c.hits.Add(1)
		c.log.Debug(ctx, "tag_gi3jxb", "response cache hit", "key", printableKey(key))
		return v, true
	}
	c.misses.Add(1)
	c.log.Debug(ctx, "tag_4hxorb", "response cache miss", "key", printableKey(key))
	return nil, false
}

func cacheKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

type printableKey string

func (k printableKey) String() string {
	return strings.ReplaceAll(string(k), "\x00", "|")
}

func (c *CachedEngine) SaveNamespace(ctx context.Context, ns model.Namespace) error {
	err := c.ReBACEngine.SaveNamespace(ctx, ns)
	if err == nil {
		c.log.Info(ctx, "tag_zxcmtx", "clearing response cache after namespace change", "namespace", ns.Name, "op", "save")
		c.cache.Clear(ctx)
	}
	return err
}

func (c *CachedEngine) DeleteNamespace(ctx context.Context, name string) error {
	err := c.ReBACEngine.DeleteNamespace(ctx, name)
	if err == nil {
		c.log.Info(ctx, "tag_wgpeu5", "clearing response cache after namespace change", "namespace", name, "op", "delete")
		c.cache.Clear(ctx)
	}
	return err
}

func (c *CachedEngine) Check(ctx context.Context, obj model.Entity, relation string, sub model.Entity) (bool, error) {
	key := cacheKey("c", obj.String(), relation, sub.String())
	if v, ok := c.lookupCache(ctx, key); ok {
		return v.(bool), nil
	}
	ok, err := c.ReBACEngine.Check(ctx, obj, relation, sub)
	if err == nil {
		c.cache.Set(ctx, key, ok)
	}
	return ok, err
}

func (c *CachedEngine) Expand(ctx context.Context, obj model.Entity, relation string) ([]model.Entity, error) {
	key := cacheKey("e", obj.String(), relation)
	if v, ok := c.lookupCache(ctx, key); ok {
		return v.([]model.Entity), nil
	}
	out, err := c.ReBACEngine.Expand(ctx, obj, relation)
	if err == nil {
		c.cache.Set(ctx, key, out)
	}
	return out, err
}

func (c *CachedEngine) Lookup(ctx context.Context, sub model.Entity, relation string, namespace string, page Page) ([]model.Entity, string, error) {
	key := cacheKey("l", sub.String(), relation, namespace, page.After, strconv.Itoa(page.Limit))
	if v, ok := c.lookupCache(ctx, key); ok {
		r := v.(lookupResult)
		return r.objects, r.next, nil
	}
	objs, next, err := c.ReBACEngine.Lookup(ctx, sub, relation, namespace, page)
	if err == nil {
		c.cache.Set(ctx, key, lookupResult{objects: objs, next: next})
	}
	return objs, next, err
}
