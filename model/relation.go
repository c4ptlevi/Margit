package model

import (
	"fmt"

	"github.com/c4ptlevi/margit/ast"
)

type Relation struct {
	Name         string
	AllowedTypes []string
	RelExpr      string
}

func (r Relation) IsDirect() bool { return r.RelExpr == "" }

func (r Relation) Expr() (ast.Node, error) {
	n, err := ast.Parse(r.RelExpr)
	if err != nil {
		return nil, fmt.Errorf("%w: relation %q: %w", ErrInvalidExpression, r.Name, err)
	}
	return n, nil
}

func (r Relation) Validate() error {
	_, err := r.validate()
	return err
}

func (r Relation) validate() (ast.Node, error) {
	if !ast.IsIdent(r.Name) {
		return nil, fmt.Errorf("%w: relation %q", ErrInvalidIdentifier, r.Name)
	}
	if (len(r.AllowedTypes) == 0) == (r.RelExpr == "") {
		return nil, fmt.Errorf("%w: relation %q", ErrInvalidRelationKind, r.Name)
	}
	seen := make(map[string]bool, len(r.AllowedTypes))
	for _, t := range r.AllowedTypes {
		if !ast.IsIdent(t) {
			return nil, fmt.Errorf("%w: type %q in relation %q", ErrInvalidIdentifier, t, r.Name)
		}
		if seen[t] {
			return nil, fmt.Errorf("%w: %q in relation %q", ErrDuplicateType, t, r.Name)
		}
		seen[t] = true
	}
	if r.IsDirect() {
		return nil, nil
	}
	return r.Expr()
}
