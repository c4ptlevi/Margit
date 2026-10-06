package model

import (
	"fmt"

	"github.com/c4ptlevi/margit/ast"
)

type Entity struct {
	Namespace string
	ID        string
}

func (e Entity) String() string { return e.Namespace + ":" + e.ID }

func (e Entity) Validate() error {
	if !ast.IsIdent(e.Namespace) {
		return fmt.Errorf("%w: namespace %q", ErrInvalidIdentifier, e.Namespace)
	}
	if e.ID == "" {
		return fmt.Errorf("%w: %s", ErrEmptyID, e.Namespace)
	}
	return nil
}
