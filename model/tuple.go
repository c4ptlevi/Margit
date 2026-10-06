package model

import (
	"fmt"
	"slices"

	"github.com/c4ptlevi/margit/ast"
)

type RelationTuple struct {
	Object   Entity
	Relation string
	Subject  Entity
}

func (t RelationTuple) String() string {
	return t.Object.String() + "#" + t.Relation + "@" + t.Subject.String()
}

func (t RelationTuple) Validate() error {
	if err := t.Object.Validate(); err != nil {
		return fmt.Errorf("object: %w", err)
	}
	if !ast.IsIdent(t.Relation) {
		return fmt.Errorf("%w: relation %q", ErrInvalidIdentifier, t.Relation)
	}
	if err := t.Subject.Validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	return nil
}

func (t RelationTuple) ValidateAgainst(ns Namespace) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if t.Object.Namespace != ns.Name {
		return fmt.Errorf("%w: tuple %s, namespace %q", ErrNamespaceMismatch, t, ns.Name)
	}
	r, err := ns.Relation(t.Relation)
	if err != nil {
		return err
	}
	if !r.IsDirect() {
		return fmt.Errorf("%w: %s#%s", ErrRelationNotDirect, ns.Name, r.Name)
	}
	if !slices.Contains(r.AllowedTypes, t.Subject.Namespace) {
		return fmt.Errorf("%w: %q for %s#%s", ErrSubjectTypeNotAllowed, t.Subject.Namespace, ns.Name, r.Name)
	}
	return nil
}
