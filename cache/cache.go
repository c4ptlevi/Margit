package cache

import (
	"context"
	"hash/maphash"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c4ptlevi/margit/logger"
)

const (
	DefaultMaxEntries = 1_000_000
	Shards            = 64
)

type Config struct {
	TTLMillis  int `json:"ttl_ms"`
	MaxEntries int `json:"max_entries"`
}

func (c Config) Enabled() bool { return c.TTLMillis > 0 }

type Cache interface {
	Get(ctx context.Context, key string) (any, bool)
	Set(ctx context.Context, key string, v any)
	Delete(ctx context.Context, key string)
	Clear(ctx context.Context)
	Len(ctx context.Context) int
}

type Stats struct {
	Hits, Misses, Bypasses uint64
	Entries                int
}

type Statser interface {
	CacheStats() Stats
}

type Counters struct {
	hits, misses, bypasses atomic.Uint64
}

func (c *Counters) Hit()    { c.hits.Add(1) }
func (c *Counters) Miss()   { c.misses.Add(1) }
func (c *Counters) Bypass() { c.bypasses.Add(1) }

func (c *Counters) Stats(entries int) Stats {
	return Stats{Hits: c.hits.Load(), Misses: c.misses.Load(), Bypasses: c.bypasses.Load(), Entries: entries}
}

type bypassKey struct{}

func WithBypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, bypassKey{}, true)
}

func Bypassed(ctx context.Context) bool {
	b, _ := ctx.Value(bypassKey{}).(bool)
	return b
}

var _ Cache = (*Mem)(nil)

type entry struct {
	v   any
	exp int64
}

type shard struct {
	mu sync.Mutex
	m  map[string]entry
}

type Mem struct {
	Now      func() time.Time
	name     string
	seed     maphash.Seed
	ttl      time.Duration
	perShard int
	shards   [Shards]shard
	log      *logger.Logger
}

func NewMem(ctx context.Context, name string, cfg Config, log *logger.Logger) *Mem {
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = DefaultMaxEntries
	}
	c := &Mem{
		Now:      time.Now,
		name:     name,
		seed:     maphash.MakeSeed(),
		ttl:      time.Duration(cfg.TTLMillis) * time.Millisecond,
		perShard: max(1, cfg.MaxEntries/Shards),
		log:      log,
	}
	for i := range c.shards {
		c.shards[i].m = make(map[string]entry)
	}
	log.Info(ctx, "tag_k53qlw", "cache created", "cache", name, "ttl", c.ttl, "max_entries", cfg.MaxEntries, "shards", Shards)
	return c
}

func (c *Mem) shard(key string) *shard {
	return &c.shards[maphash.String(c.seed, key)%Shards]
}

func (c *Mem) Get(_ context.Context, key string) (any, bool) {
	s := c.shard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok {
		return nil, false
	}
	if c.Now().UnixNano() >= e.exp {
		delete(s.m, key)
		return nil, false
	}
	return e.v, true
}

func (c *Mem) Set(ctx context.Context, key string, v any) {
	s := c.shard(key)
	now := c.Now().UnixNano()
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
		c.log.Debug(ctx, "tag_ybnsxs", "cache shard evicted", "cache", c.name, "before", before, "after", len(s.m), "limit", c.perShard)
	}
	s.m[key] = entry{v: v, exp: now + int64(c.ttl)}
}

func (c *Mem) Delete(_ context.Context, key string) {
	s := c.shard(key)
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
}

func (c *Mem) Clear(ctx context.Context) {
	n := 0
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		n += len(s.m)
		s.m = make(map[string]entry)
		s.mu.Unlock()
	}
	c.log.Info(ctx, "tag_ul6b0x", "cache cleared", "cache", c.name, "entries", n)
}

func (c *Mem) Len(context.Context) int {
	n := 0
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		n += len(s.m)
		s.mu.Unlock()
	}
	return n
}
