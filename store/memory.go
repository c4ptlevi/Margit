package store

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
)

var _ Store = (*MemoryStore)(nil)

type edgeKey struct {
	entity   model.Entity
	relation string
}

type entitySet map[model.Entity]struct{}

type MemoryStore struct {
	mu         sync.RWMutex
	namespaces map[string]model.Namespace
	forward    map[edgeKey]entitySet
	reverse    map[edgeKey]entitySet
	log        *logger.Logger
}

func NewMemoryStore(log *logger.Logger) *MemoryStore {
	return &MemoryStore{
		namespaces: make(map[string]model.Namespace),
		forward:    make(map[edgeKey]entitySet),
		reverse:    make(map[edgeKey]entitySet),
		log:        log,
	}
}

func (s *MemoryStore) SaveNamespace(ctx context.Context, ns model.Namespace) error {
	s.mu.Lock()
	s.namespaces[ns.Name] = cloneNamespace(ns)
	s.mu.Unlock()
	s.log.Debug(ctx, "tag_j5eing", "namespace saved", "namespace", ns.Name, "relations", len(ns.Relations))
	return nil
}

func (s *MemoryStore) GetNamespace(_ context.Context, name string) (model.Namespace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ns, ok := s.namespaces[name]
	if !ok {
		return model.Namespace{}, fmt.Errorf("%w: namespace %q", model.ErrNotFound, name)
	}
	return cloneNamespace(ns), nil
}

func (s *MemoryStore) ListNamespaces(_ context.Context) ([]model.Namespace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Namespace, 0, len(s.namespaces))
	for _, name := range slices.Sorted(maps.Keys(s.namespaces)) {
		out = append(out, cloneNamespace(s.namespaces[name]))
	}
	return out, nil
}

func (s *MemoryStore) DeleteNamespace(ctx context.Context, name string) error {
	s.mu.Lock()
	_, ok := s.namespaces[name]
	delete(s.namespaces, name)
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: namespace %q", model.ErrNotFound, name)
	}
	s.log.Debug(ctx, "tag_19nksa", "namespace deleted", "namespace", name)
	return nil
}

func (s *MemoryStore) NamespaceExists(_ context.Context, name string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.namespaces[name]
	return ok, nil
}

func (s *MemoryStore) RelationExists(_ context.Context, namespace string, relation string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.namespaces[namespace].Relations[relation]
	return ok, nil
}

func (s *MemoryStore) WriteTuples(ctx context.Context, tuples []model.RelationTuple) error {
	s.mu.Lock()
	for _, t := range tuples {
		add(s.forward, edgeKey{t.Object, t.Relation}, t.Subject)
		add(s.reverse, edgeKey{t.Subject, t.Relation}, t.Object)
	}
	s.mu.Unlock()
	s.log.Debug(ctx, "tag_nyrgo7", "tuples written", "count", len(tuples))
	return nil
}

func (s *MemoryStore) DeleteTuples(ctx context.Context, tuples []model.RelationTuple) error {
	s.mu.Lock()
	for _, t := range tuples {
		remove(s.forward, edgeKey{t.Object, t.Relation}, t.Subject)
		remove(s.reverse, edgeKey{t.Subject, t.Relation}, t.Object)
	}
	s.mu.Unlock()
	s.log.Debug(ctx, "tag_q305ev", "tuples deleted", "count", len(tuples))
	return nil
}

func (s *MemoryStore) ReadTuples(_ context.Context, obj model.Entity, relation string) ([]model.RelationTuple, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	subjects := s.forward[edgeKey{obj, relation}]
	out := make([]model.RelationTuple, 0, len(subjects))
	for sub := range subjects {
		out = append(out, model.RelationTuple{Object: obj, Relation: relation, Subject: sub})
	}
	return out, nil
}

func (s *MemoryStore) ReadTuplesBySubject(_ context.Context, sub model.Entity, relation string) ([]model.RelationTuple, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	objects := s.reverse[edgeKey{sub, relation}]
	out := make([]model.RelationTuple, 0, len(objects))
	for obj := range objects {
		out = append(out, model.RelationTuple{Object: obj, Relation: relation, Subject: sub})
	}
	return out, nil
}

func (s *MemoryStore) ForEachTuple(_ context.Context, fn func(model.RelationTuple) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, subjects := range s.forward {
		for sub := range subjects {
			if err := fn(model.RelationTuple{Object: k.entity, Relation: k.relation, Subject: sub}); err != nil {
				return err
			}
		}
	}
	return nil
}

func add(index map[edgeKey]entitySet, k edgeKey, e model.Entity) {
	set, ok := index[k]
	if !ok {
		set = make(entitySet)
		index[k] = set
	}
	set[e] = struct{}{}
}

func remove(index map[edgeKey]entitySet, k edgeKey, e model.Entity) {
	set := index[k]
	delete(set, e)
	if len(set) == 0 {
		delete(index, k)
	}
}

func cloneNamespace(ns model.Namespace) model.Namespace {
	rels := make(map[string]model.Relation, len(ns.Relations))
	for name, r := range ns.Relations {
		r.AllowedTypes = slices.Clone(r.AllowedTypes)
		rels[name] = r
	}
	ns.Relations = rels
	return ns
}
