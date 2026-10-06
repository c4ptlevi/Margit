package model

import (
	"errors"
	"fmt"

	"github.com/c4ptlevi/margit/ast"
)

type Namespace struct {
	Name      string
	Relations map[string]Relation
}

func (ns Namespace) Relation(name string) (Relation, error) {
	r, ok := ns.Relations[name]
	if !ok {
		return Relation{}, fmt.Errorf("%w: %s#%s", ErrUnknownRelation, ns.Name, name)
	}
	return r, nil
}

func (ns Namespace) Validate(get func(name string) (Namespace, error)) error {
	if !ast.IsIdent(ns.Name) {
		return fmt.Errorf("%w: namespace %q", ErrInvalidIdentifier, ns.Name)
	}
	exprs := make(map[string]ast.Node)
	for name, r := range ns.Relations {
		if r.Name != name {
			return fmt.Errorf("%w: key %q, name %q in namespace %q", ErrRelationNameMismatch, name, r.Name, ns.Name)
		}
		expr, err := r.validate()
		if err != nil {
			return fmt.Errorf("namespace %q: %w", ns.Name, err)
		}
		if expr != nil {
			exprs[name] = expr
		}
	}

	fetched := map[string]Namespace{ns.Name: ns}
	fetch := func(name string) (Namespace, error) {
		if n, ok := fetched[name]; ok {
			return n, nil
		}
		n, err := get(name)
		if errors.Is(err, ErrNotFound) {
			return Namespace{}, fmt.Errorf("%w: %q", ErrUnknownNamespace, name)
		}
		if err != nil {
			return Namespace{}, err
		}
		fetched[name] = n
		return n, nil
	}

	for name, r := range ns.Relations {
		for _, t := range r.AllowedTypes {
			if _, err := fetch(t); err != nil {
				return fmt.Errorf("%s#%s: %w", ns.Name, name, err)
			}
		}
	}

	for name, expr := range exprs {
		err := ast.Walk(expr, func(n ast.Node) error {
			switch n := n.(type) {
			case ast.Computed:
				if _, ok := ns.Relations[n.Relation]; !ok {
					return fmt.Errorf("%w: %s#%s referenced by %s", ErrUnknownRelation, ns.Name, n.Relation, name)
				}
			case ast.Arrow:
				ts, ok := ns.Relations[n.Tupleset]
				if !ok {
					return fmt.Errorf("%w: %s#%s referenced by %s", ErrUnknownRelation, ns.Name, n.Tupleset, name)
				}
				if !ts.IsDirect() {
					return fmt.Errorf("%w: %s#%s used in %s", ErrInvalidTupleset, ns.Name, n.Tupleset, name)
				}
				for _, t := range ts.AllowedTypes {
					target, err := fetch(t)
					if err != nil {
						return err
					}
					if _, ok := target.Relations[n.Relation]; !ok {
						return fmt.Errorf("%w: %s#%s referenced by %s#%s", ErrUnknownRelation, t, n.Relation, ns.Name, name)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return checkCycles(ns.Name, exprs)
}

func checkCycles(nsName string, exprs map[string]ast.Node) error {
	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(exprs))
	var visit func(name string) error
	visit = func(name string) error {
		switch state[name] {
		case visiting:
			return fmt.Errorf("%w: %s#%s", ErrCyclicRelation, nsName, name)
		case done:
			return nil
		}
		state[name] = visiting
		if expr, ok := exprs[name]; ok {
			err := ast.Walk(expr, func(n ast.Node) error {
				if c, ok := n.(ast.Computed); ok {
					return visit(c.Relation)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		state[name] = done
		return nil
	}
	for name := range exprs {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}
