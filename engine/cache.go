package engine

import (
	"context"
	"sync"

	"github.com/c4ptlevi/margit/ast"
	"github.com/c4ptlevi/margit/model"
)

type ExprCache interface {
	Get(ctx context.Context, r model.Relation) (ast.Node, error)
}

var _ ExprCache = (*MemExprCache)(nil)

type MemExprCache struct {
	m sync.Map
}

func (c *MemExprCache) Get(_ context.Context, r model.Relation) (ast.Node, error) {
	if n, ok := c.m.Load(r.RelExpr); ok {
		return n.(ast.Node), nil
	}
	n, err := r.Expr()
	if err != nil {
		return nil, err
	}
	actual, _ := c.m.LoadOrStore(r.RelExpr, n)
	return actual.(ast.Node), nil
}
