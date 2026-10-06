package engine

import (
	"context"
	"strconv"
	"strings"

	"github.com/c4ptlevi/margit/cache"
	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
)

type CacheConfig = cache.Config

type ResponseCache = cache.Cache

type CacheStats = cache.Stats

type CacheStatser = cache.Statser

type Consistency string

const (
	MinimizeLatency Consistency = "minimize_latency"
	FullyConsistent Consistency = "full"
)

type consistencyKey struct{}

func WithConsistency(ctx context.Context, c Consistency) context.Context {
	ctx = context.WithValue(ctx, consistencyKey{}, c)
	if c == FullyConsistent {
		ctx = cache.WithBypass(ctx)
	}
	return ctx
}

func consistencyFrom(ctx context.Context) Consistency {
	c, _ := ctx.Value(consistencyKey{}).(Consistency)
	return c
}

func NewMemResponseCache(ctx context.Context, cfg CacheConfig, log *logger.Logger) *cache.Mem {
	return cache.NewMem(ctx, "response", cfg, log)
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
	cache ResponseCache
	stats cache.Counters
	log   *logger.Logger
}

func NewCached(inner ReBACEngine, cache ResponseCache, log *logger.Logger) *CachedEngine {
	return &CachedEngine{ReBACEngine: inner, cache: cache, log: log}
}

func (c *CachedEngine) CacheStats() CacheStats {
	return c.stats.Stats(c.cache.Len(context.Background()))
}

func (c *CachedEngine) lookupCache(ctx context.Context, key string) (any, bool) {
	if consistencyFrom(ctx) == FullyConsistent {
		c.stats.Bypass()
		c.log.Debug(ctx, "tag_1odurh", "response cache bypassed", "key", printableKey(key))
		return nil, false
	}
	if v, ok := c.cache.Get(ctx, key); ok {
		c.stats.Hit()
		c.log.Debug(ctx, "tag_gi3jxb", "response cache hit", "key", printableKey(key))
		return v, true
	}
	c.stats.Miss()
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
