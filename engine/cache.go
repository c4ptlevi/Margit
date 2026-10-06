package engine

import (
	"context"
	"sync"

	"github.com/c4ptlevi/margit/ast"
	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
)

type ExprCache interface {
	Get(ctx context.Context, r model.Relation) (ast.Node, error)
}

var _ ExprCache = (*MemExprCache)(nil)

type MemExprCache struct {
	Log *logger.Logger
	m   sync.Map
}

func (c *MemExprCache) Get(ctx context.Context, r model.Relation) (ast.Node, error) {
	if n, ok := c.m.Load(r.RelExpr); ok {
		return n.(ast.Node), nil
	}
	n, err := r.Expr()
	if err != nil {
		c.Log.Debug(ctx, "tag_fpwyse", "relation expression parse failed", "relation", r.Name, "expr", r.RelExpr, "err", err)
		return nil, err
	}
	actual, loaded := c.m.LoadOrStore(r.RelExpr, n)
	if !loaded {
		c.Log.Debug(ctx, "tag_hc0i5k", "relation expression compiled", "relation", r.Name, "expr", r.RelExpr)
	}
	return actual.(ast.Node), nil
}
