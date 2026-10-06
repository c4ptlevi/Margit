package store

import (
	"context"
	"strings"

	"github.com/c4ptlevi/margit/cache"
	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
)

var (
	_ Store         = (*CachedStore)(nil)
	_ cache.Statser = (*CachedStore)(nil)
)

type CachedStore struct {
	Store
	cache cache.Cache
	stats cache.Counters
	log   *logger.Logger
}

func NewCached(inner Store, c cache.Cache, log *logger.Logger) *CachedStore {
	return &CachedStore{Store: inner, cache: c, log: log}
}

func (s *CachedStore) CacheStats() cache.Stats {
	return s.stats.Stats(s.cache.Len(context.Background()))
}

func key(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func objectKey(obj model.Entity, relation string) string {
	return key("o", obj.Namespace, obj.ID, relation)
}

func subjectKey(sub model.Entity, relation string) string {
	return key("s", sub.Namespace, sub.ID, relation)
}

func cached[T any](ctx context.Context, s *CachedStore, k string, read func() (T, error)) (T, error) {
	debug := s.log.Enabled(logger.LevelDebug)
	if cache.Bypassed(ctx) {
		s.stats.Bypass()
		if debug {
			s.log.Debug(ctx, "tag_lcgflp", "query cache bypassed", "key", printable(k))
		}
	} else if v, ok := s.cache.Get(ctx, k); ok {
		s.stats.Hit()
		if debug {
			s.log.Debug(ctx, "tag_j73o90", "query cache hit", "key", printable(k))
		}
		return v.(T), nil
	} else {
		s.stats.Miss()
		if debug {
			s.log.Debug(ctx, "tag_ms6ias", "query cache miss", "key", printable(k))
		}
	}
	v, err := read()
	if err == nil {
		s.cache.Set(ctx, k, v)
	}
	return v, err
}

func printable(k string) string {
	return strings.ReplaceAll(k, "\x00", "|")
}

func (s *CachedStore) GetNamespace(ctx context.Context, name string) (model.Namespace, error) {
	return cached(ctx, s, key("n", name), func() (model.Namespace, error) { return s.Store.GetNamespace(ctx, name) })
}

func (s *CachedStore) NamespaceExists(ctx context.Context, name string) (bool, error) {
	return cached(ctx, s, key("ne", name), func() (bool, error) { return s.Store.NamespaceExists(ctx, name) })
}

func (s *CachedStore) RelationExists(ctx context.Context, namespace, relation string) (bool, error) {
	return cached(ctx, s, key("re", namespace, relation), func() (bool, error) { return s.Store.RelationExists(ctx, namespace, relation) })
}

func (s *CachedStore) ReadTuples(ctx context.Context, obj model.Entity, relation string) ([]model.RelationTuple, error) {
	return cached(ctx, s, objectKey(obj, relation), func() ([]model.RelationTuple, error) { return s.Store.ReadTuples(ctx, obj, relation) })
}

func (s *CachedStore) ReadTuplesBySubject(ctx context.Context, sub model.Entity, relation string) ([]model.RelationTuple, error) {
	return cached(ctx, s, subjectKey(sub, relation), func() ([]model.RelationTuple, error) { return s.Store.ReadTuplesBySubject(ctx, sub, relation) })
}

func (s *CachedStore) SaveNamespace(ctx context.Context, ns model.Namespace) error {
	defer s.clear(ctx, "namespace saved", ns.Name)
	return s.Store.SaveNamespace(ctx, ns)
}

func (s *CachedStore) DeleteNamespace(ctx context.Context, name string) error {
	defer s.clear(ctx, "namespace deleted", name)
	return s.Store.DeleteNamespace(ctx, name)
}

func (s *CachedStore) WriteTuples(ctx context.Context, tuples []model.RelationTuple) error {
	defer s.invalidate(ctx, "write", tuples)
	return s.Store.WriteTuples(ctx, tuples)
}

func (s *CachedStore) DeleteTuples(ctx context.Context, tuples []model.RelationTuple) error {
	defer s.invalidate(ctx, "delete", tuples)
	return s.Store.DeleteTuples(ctx, tuples)
}

func (s *CachedStore) clear(ctx context.Context, reason, namespace string) {
	s.log.Info(ctx, "tag_q6l5i9", "clearing query cache", "reason", reason, "namespace", namespace)
	s.cache.Clear(ctx)
}

func (s *CachedStore) invalidate(ctx context.Context, op string, tuples []model.RelationTuple) {
	for _, t := range tuples {
		s.cache.Delete(ctx, objectKey(t.Object, t.Relation))
		s.cache.Delete(ctx, subjectKey(t.Subject, t.Relation))
	}
	s.log.Debug(ctx, "tag_2wfbvy", "query cache invalidated", "op", op, "tuples", len(tuples), "keys", 2*len(tuples))
}
