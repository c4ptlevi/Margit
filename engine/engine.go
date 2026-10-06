package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/c4ptlevi/margit/ast"
	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
	"github.com/c4ptlevi/margit/store"
)

const (
	DefaultMaxDepth       = 25
	DefaultLookupLimit    = 100
	DefaultMaxLookupLimit = 1000
)

type ReBACEngine interface {
	SaveNamespace(ctx context.Context, ns model.Namespace) error
	GetNamespace(ctx context.Context, name string) (model.Namespace, error)
	ListNamespaces(ctx context.Context) ([]model.Namespace, error)
	DeleteNamespace(ctx context.Context, name string) error
	WriteTuples(ctx context.Context, tuples []model.RelationTuple) error
	DeleteTuples(ctx context.Context, tuples []model.RelationTuple) error
	Check(ctx context.Context, obj model.Entity, relation string, sub model.Entity) (bool, error)
	Expand(ctx context.Context, obj model.Entity, relation string) ([]model.Entity, error)
	Lookup(ctx context.Context, sub model.Entity, relation string, namespace string, page Page) ([]model.Entity, string, error)
}

type Config struct {
	MaxDepth       int         `json:"max_depth"`
	LookupLimit    int         `json:"lookup_default_limit"`
	MaxLookupLimit int         `json:"lookup_max_limit"`
	Cache          CacheConfig `json:"response_cache"`
}

type Page struct {
	After string
	Limit int
}

var _ ReBACEngine = (*Engine)(nil)

type Engine struct {
	store          store.Store
	exprs          ExprCache
	maxDepth       int
	lookupLimit    int
	maxLookupLimit int
	log            *logger.Logger
}

func New(st store.Store, exprs ExprCache, cfg Config, log *logger.Logger) *Engine {
	if exprs == nil {
		exprs = &MemExprCache{}
	}
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = DefaultMaxDepth
	}
	if cfg.MaxLookupLimit <= 0 {
		cfg.MaxLookupLimit = DefaultMaxLookupLimit
	}
	if cfg.LookupLimit <= 0 {
		cfg.LookupLimit = DefaultLookupLimit
	}
	cfg.LookupLimit = min(cfg.LookupLimit, cfg.MaxLookupLimit)
	return &Engine{
		store:          st,
		exprs:          exprs,
		maxDepth:       cfg.MaxDepth,
		lookupLimit:    cfg.LookupLimit,
		maxLookupLimit: cfg.MaxLookupLimit,
		log:            log,
	}
}

func (e *Engine) SaveNamespace(ctx context.Context, ns model.Namespace) error {
	err := ns.Validate(func(name string) (model.Namespace, error) {
		return e.store.GetNamespace(ctx, name)
	})
	if err != nil {
		e.log.Warn(ctx, "tag_aq50uv", "namespace rejected", "namespace", ns.Name, "err", err)
		return err
	}
	if err := e.store.SaveNamespace(ctx, ns); err != nil {
		e.log.Warn(ctx, "tag_abd4sb", "namespace save failed", "namespace", ns.Name, "err", err)
		return err
	}
	e.log.Info(ctx, "tag_5b856o", "namespace saved", "namespace", ns.Name, "relations", len(ns.Relations))
	return nil
}

func (e *Engine) GetNamespace(ctx context.Context, name string) (model.Namespace, error) {
	return e.store.GetNamespace(ctx, name)
}

func (e *Engine) ListNamespaces(ctx context.Context) ([]model.Namespace, error) {
	return e.store.ListNamespaces(ctx)
}

func (e *Engine) DeleteNamespace(ctx context.Context, name string) error {
	all, err := e.store.ListNamespaces(ctx)
	if err != nil {
		return err
	}
	for _, ns := range all {
		if ns.Name == name {
			continue
		}
		for _, r := range ns.Relations {
			if slices.Contains(r.AllowedTypes, name) {
				err := fmt.Errorf("%w: %s used by %s#%s", model.ErrNamespaceInUse, name, ns.Name, r.Name)
				e.log.Warn(ctx, "tag_jx6zju", "namespace delete rejected", "namespace", name, "err", err)
				return err
			}
		}
	}
	if err := e.store.DeleteNamespace(ctx, name); err != nil {
		e.log.Debug(ctx, "tag_h758ev", "namespace delete failed", "namespace", name, "err", err)
		return err
	}
	e.log.Info(ctx, "tag_1wwt03", "namespace deleted", "namespace", name)
	return nil
}

func (e *Engine) WriteTuples(ctx context.Context, tuples []model.RelationTuple) error {
	start := time.Now()
	r := e.newRequest()
	for _, t := range tuples {
		if err := ctx.Err(); err != nil {
			e.log.Warn(ctx, "tag_svx9qp", "tuple write cancelled", "count", len(tuples), "err", err)
			return err
		}
		ns, err := r.namespace(ctx, t.Object.Namespace)
		if err == nil {
			err = t.ValidateAgainst(ns)
		}
		if err != nil {
			e.log.Warn(ctx, "tag_odelzx", "tuple rejected", "tuple", t, "err", err)
			return fmt.Errorf("tuple %s: %w", t, err)
		}
	}
	if err := e.store.WriteTuples(ctx, tuples); err != nil {
		e.log.Warn(ctx, "tag_b7p6x2", "tuple write failed", "count", len(tuples), "err", err)
		return err
	}
	e.log.Debug(ctx, "tag_ipwl9z", "tuples written", "count", len(tuples), "took", time.Since(start))
	return nil
}

func (e *Engine) DeleteTuples(ctx context.Context, tuples []model.RelationTuple) error {
	start := time.Now()
	for _, t := range tuples {
		if err := t.Validate(); err != nil {
			e.log.Warn(ctx, "tag_c576m1", "tuple delete rejected", "tuple", t, "err", err)
			return fmt.Errorf("tuple %s: %w", t, err)
		}
	}
	if err := e.store.DeleteTuples(ctx, tuples); err != nil {
		e.log.Warn(ctx, "tag_kj7z6a", "tuple delete failed", "count", len(tuples), "err", err)
		return err
	}
	e.log.Debug(ctx, "tag_wyr26d", "tuples deleted", "count", len(tuples), "took", time.Since(start))
	return nil
}

func (e *Engine) Check(ctx context.Context, obj model.Entity, relation string, sub model.Entity) (bool, error) {
	start := time.Now()
	if err := (model.RelationTuple{Object: obj, Relation: relation, Subject: sub}).Validate(); err != nil {
		e.log.Debug(ctx, "tag_kanc1y", "check rejected", "err", err)
		return false, err
	}
	r := e.newRequest()
	if _, err := r.relation(ctx, obj.Namespace, relation); err != nil {
		e.log.Debug(ctx, "tag_fzy3io", "check rejected", "err", err)
		return false, err
	}
	ok, err := r.check(ctx, obj, relation, sub, 0)
	if err != nil {
		e.log.Warn(ctx, "tag_biw052", "check failed", "object", obj, "relation", relation, "subject", sub, "err", err)
		return false, err
	}
	e.log.Debug(ctx, "tag_suy0ou", "check done", "object", obj, "relation", relation, "subject", sub, "allowed", ok, "took", time.Since(start))
	return ok, nil
}

func (e *Engine) Expand(ctx context.Context, obj model.Entity, relation string) ([]model.Entity, error) {
	start := time.Now()
	if err := obj.Validate(); err != nil {
		e.log.Debug(ctx, "tag_4rmtvm", "expand rejected", "object", obj, "err", err)
		return nil, err
	}
	r := e.newRequest()
	if _, err := r.relation(ctx, obj.Namespace, relation); err != nil {
		e.log.Debug(ctx, "tag_iu4grf", "expand rejected", "err", err)
		return nil, err
	}
	set, err := r.expand(ctx, obj, relation, 0)
	if err != nil {
		e.log.Warn(ctx, "tag_flcq0b", "expand failed", "object", obj, "relation", relation, "err", err)
		return nil, err
	}
	out := set.sorted()
	e.log.Debug(ctx, "tag_bgnsp7", "expand done", "object", obj, "relation", relation, "count", len(out), "took", time.Since(start))
	return out, nil
}

func (e *Engine) Lookup(ctx context.Context, sub model.Entity, relation string, namespace string, page Page) ([]model.Entity, string, error) {
	start := time.Now()
	if err := sub.Validate(); err != nil {
		e.log.Debug(ctx, "tag_62wfq4", "lookup rejected", "subject", sub, "err", err)
		return nil, "", err
	}
	if page.Limit < 0 {
		err := fmt.Errorf("%w: %d", model.ErrInvalidLimit, page.Limit)
		e.log.Debug(ctx, "tag_vq06ud", "lookup rejected", "subject", sub, "err", err)
		return nil, "", err
	}
	limit := page.Limit
	if limit == 0 {
		limit = e.lookupLimit
	}
	limit = min(limit, e.maxLookupLimit)
	r := e.newRequest()
	if _, err := r.relation(ctx, namespace, relation); err != nil {
		e.log.Debug(ctx, "tag_de91dd", "lookup rejected", "err", err)
		return nil, "", err
	}
	l := &lookup{request: r, sub: sub, memo: map[nsRel]entitySet{}, active: map[nsRel]bool{}}
	var candidates entitySet
	passes := 0
	for {
		passes++
		l.changed = false
		set, err := l.candidates(ctx, namespace, relation, 0)
		if err != nil {
			e.log.Warn(ctx, "tag_kr46bk", "lookup failed", "subject", sub, "relation", relation, "namespace", namespace, "err", err)
			return nil, "", err
		}
		if !l.changed {
			candidates = set
			break
		}
	}
	var out []model.Entity
	checked := 0
	for _, obj := range candidates.sorted() {
		if obj.ID <= page.After {
			continue
		}
		checked++
		ok, err := r.check(ctx, obj, relation, sub, 0)
		if err != nil {
			e.log.Warn(ctx, "tag_43nxj0", "lookup failed", "subject", sub, "relation", relation, "namespace", namespace, "err", err)
			return nil, "", err
		}
		if ok {
			out = append(out, obj)
			if len(out) > limit {
				break
			}
		}
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].ID
	}
	e.log.Debug(ctx, "tag_ivh9li", "lookup done", "subject", sub, "relation", relation, "namespace", namespace,
		"after", page.After, "limit", limit, "passes", passes, "candidates", len(candidates), "checked", checked, "count", len(out),
		"more", next != "", "took", time.Since(start))
	return out, next, nil
}

type edge struct {
	entity   model.Entity
	relation string
}

type request struct {
	e          *Engine
	namespaces map[string]model.Namespace
	onPath     map[edge]bool
}

func (e *Engine) newRequest() *request {
	return &request{e: e, namespaces: map[string]model.Namespace{}, onPath: map[edge]bool{}}
}

func (r *request) namespace(ctx context.Context, name string) (model.Namespace, error) {
	if ns, ok := r.namespaces[name]; ok {
		return ns, nil
	}
	ns, err := r.e.store.GetNamespace(ctx, name)
	if errors.Is(err, model.ErrNotFound) {
		r.e.log.Debug(ctx, "tag_3q8bav", "unknown namespace", "namespace", name)
		return model.Namespace{}, fmt.Errorf("%w: %q", model.ErrUnknownNamespace, name)
	}
	if err != nil {
		return model.Namespace{}, err
	}
	r.namespaces[name] = ns
	return ns, nil
}

func (r *request) relation(ctx context.Context, namespace, relation string) (model.Relation, error) {
	ns, err := r.namespace(ctx, namespace)
	if err != nil {
		return model.Relation{}, err
	}
	return ns.Relation(relation)
}

func (r *request) resolve(ctx context.Context, namespace, relation string, depth int) (model.Relation, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.Relation{}, false, err
	}
	if depth > r.e.maxDepth {
		r.e.log.Debug(ctx, "tag_08soiv", "max depth exceeded", "namespace", namespace, "relation", relation, "depth", depth, "max_depth", r.e.maxDepth)
		return model.Relation{}, false, fmt.Errorf("%w: %s#%s at depth %d", model.ErrMaxDepthExceeded, namespace, relation, depth)
	}
	rel, err := r.relation(ctx, namespace, relation)
	if errors.Is(err, model.ErrUnknownNamespace) || errors.Is(err, model.ErrUnknownRelation) {
		r.e.log.Debug(ctx, "tag_rjiv2g", "missing schema reference treated as empty", "namespace", namespace, "relation", relation)
		return model.Relation{}, false, nil
	}
	if err != nil {
		return model.Relation{}, false, err
	}
	return rel, true, nil
}

func (r *request) check(ctx context.Context, obj model.Entity, relation string, sub model.Entity, depth int) (bool, error) {
	key := edge{obj, relation}
	if r.onPath[key] {
		return false, nil
	}
	rel, found, err := r.resolve(ctx, obj.Namespace, relation, depth)
	if err != nil || !found {
		return false, err
	}
	r.onPath[key] = true
	defer delete(r.onPath, key)

	if rel.IsDirect() {
		tuples, err := r.e.store.ReadTuples(ctx, obj, relation)
		if err != nil {
			return false, err
		}
		for _, t := range tuples {
			if t.Subject == sub {
				return true, nil
			}
		}
		return false, nil
	}
	expr, err := r.e.exprs.Get(ctx, rel)
	if err != nil {
		return false, err
	}
	return r.checkExpr(ctx, obj, expr, sub, depth)
}

func (r *request) checkExpr(ctx context.Context, obj model.Entity, n ast.Node, sub model.Entity, depth int) (bool, error) {
	switch n := n.(type) {
	case ast.Computed:
		return r.check(ctx, obj, n.Relation, sub, depth+1)
	case ast.Arrow:
		tuples, err := r.e.store.ReadTuples(ctx, obj, n.Tupleset)
		if err != nil {
			return false, err
		}
		for _, t := range tuples {
			ok, err := r.check(ctx, t.Subject, n.Relation, sub, depth+1)
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	case ast.Union:
		for _, c := range n.Children {
			ok, err := r.checkExpr(ctx, obj, c, sub, depth)
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	case ast.Intersection:
		for _, c := range n.Children {
			ok, err := r.checkExpr(ctx, obj, c, sub, depth)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	case ast.Exclusion:
		ok, err := r.checkExpr(ctx, obj, n.Base, sub, depth)
		if err != nil || !ok {
			return false, err
		}
		excluded, err := r.checkExpr(ctx, obj, n.Subtract, sub, depth)
		return !excluded && err == nil, err
	}
	return false, fmt.Errorf("unsupported expression node %T", n)
}

func (r *request) expand(ctx context.Context, obj model.Entity, relation string, depth int) (entitySet, error) {
	key := edge{obj, relation}
	if r.onPath[key] {
		return entitySet{}, nil
	}
	rel, found, err := r.resolve(ctx, obj.Namespace, relation, depth)
	if err != nil || !found {
		return entitySet{}, err
	}
	r.onPath[key] = true
	defer delete(r.onPath, key)

	if rel.IsDirect() {
		tuples, err := r.e.store.ReadTuples(ctx, obj, relation)
		if err != nil {
			return nil, err
		}
		out := make(entitySet, len(tuples))
		for _, t := range tuples {
			out.add(t.Subject)
		}
		return out, nil
	}
	expr, err := r.e.exprs.Get(ctx, rel)
	if err != nil {
		return nil, err
	}
	return r.expandExpr(ctx, obj, expr, depth)
}

func (r *request) expandExpr(ctx context.Context, obj model.Entity, n ast.Node, depth int) (entitySet, error) {
	switch n := n.(type) {
	case ast.Computed:
		return r.expand(ctx, obj, n.Relation, depth+1)
	case ast.Arrow:
		tuples, err := r.e.store.ReadTuples(ctx, obj, n.Tupleset)
		if err != nil {
			return nil, err
		}
		out := entitySet{}
		for _, t := range tuples {
			set, err := r.expand(ctx, t.Subject, n.Relation, depth+1)
			if err != nil {
				return nil, err
			}
			out.addAll(set)
		}
		return out, nil
	case ast.Union:
		out := entitySet{}
		for _, c := range n.Children {
			set, err := r.expandExpr(ctx, obj, c, depth)
			if err != nil {
				return nil, err
			}
			out.addAll(set)
		}
		return out, nil
	case ast.Intersection:
		var out entitySet
		for i, c := range n.Children {
			set, err := r.expandExpr(ctx, obj, c, depth)
			if err != nil {
				return nil, err
			}
			if i == 0 {
				out = set.clone()
			} else {
				out.retain(set)
			}
		}
		return out, nil
	case ast.Exclusion:
		base, err := r.expandExpr(ctx, obj, n.Base, depth)
		if err != nil {
			return nil, err
		}
		sub, err := r.expandExpr(ctx, obj, n.Subtract, depth)
		if err != nil {
			return nil, err
		}
		out := base.clone()
		out.removeAll(sub)
		return out, nil
	}
	return nil, fmt.Errorf("unsupported expression node %T", n)
}

type nsRel struct {
	namespace, relation string
}

type lookup struct {
	*request
	sub     model.Entity
	memo    map[nsRel]entitySet
	active  map[nsRel]bool
	changed bool
}

func (l *lookup) candidates(ctx context.Context, namespace, relation string, depth int) (entitySet, error) {
	key := nsRel{namespace, relation}
	if l.active[key] {
		return l.memo[key], nil
	}
	rel, found, err := l.resolve(ctx, namespace, relation, depth)
	if err != nil || !found {
		return entitySet{}, err
	}
	l.active[key] = true
	defer delete(l.active, key)

	var got entitySet
	if rel.IsDirect() {
		got, err = l.objectsOf(ctx, l.sub, relation, namespace)
	} else {
		var expr ast.Node
		expr, err = l.e.exprs.Get(ctx, rel)
		if err == nil {
			got, err = l.candidatesExpr(ctx, namespace, expr, depth)
		}
	}
	if err != nil {
		return nil, err
	}

	memo := l.memo[key]
	if memo == nil {
		memo = entitySet{}
		l.memo[key] = memo
	}
	for e := range got {
		if !memo.has(e) {
			memo.add(e)
			l.changed = true
		}
	}
	return memo, nil
}

func (l *lookup) candidatesExpr(ctx context.Context, namespace string, n ast.Node, depth int) (entitySet, error) {
	switch n := n.(type) {
	case ast.Computed:
		return l.candidates(ctx, namespace, n.Relation, depth+1)
	case ast.Arrow:
		ts, found, err := l.resolve(ctx, namespace, n.Tupleset, depth)
		if err != nil || !found {
			return entitySet{}, err
		}
		out := entitySet{}
		for _, t := range ts.AllowedTypes {
			mids, err := l.candidates(ctx, t, n.Relation, depth+1)
			if err != nil {
				return nil, err
			}
			for mid := range mids {
				objs, err := l.objectsOf(ctx, mid, n.Tupleset, namespace)
				if err != nil {
					return nil, err
				}
				out.addAll(objs)
			}
		}
		return out, nil
	case ast.Union:
		out := entitySet{}
		for _, c := range n.Children {
			set, err := l.candidatesExpr(ctx, namespace, c, depth)
			if err != nil {
				return nil, err
			}
			out.addAll(set)
		}
		return out, nil
	case ast.Intersection:
		var out entitySet
		for i, c := range n.Children {
			set, err := l.candidatesExpr(ctx, namespace, c, depth)
			if err != nil {
				return nil, err
			}
			if i == 0 {
				out = set.clone()
			} else {
				out.retain(set)
			}
		}
		return out, nil
	case ast.Exclusion:
		return l.candidatesExpr(ctx, namespace, n.Base, depth)
	}
	return nil, fmt.Errorf("unsupported expression node %T", n)
}

func (l *lookup) objectsOf(ctx context.Context, sub model.Entity, relation, namespace string) (entitySet, error) {
	tuples, err := l.e.store.ReadTuplesBySubject(ctx, sub, relation)
	if err != nil {
		return nil, err
	}
	out := entitySet{}
	for _, t := range tuples {
		if t.Object.Namespace == namespace {
			out.add(t.Object)
		}
	}
	return out, nil
}
