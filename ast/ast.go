package ast

import "strings"

type Node interface {
	node()
	String() string
}

type Computed struct {
	Relation string
}

type Arrow struct {
	Tupleset string
	Relation string
}

type Union struct {
	Children []Node
}

type Intersection struct {
	Children []Node
}

type Exclusion struct {
	Base     Node
	Subtract Node
}

func (Computed) node()     {}
func (Arrow) node()        {}
func (Union) node()        {}
func (Intersection) node() {}
func (Exclusion) node()    {}

func (n Computed) String() string { return n.Relation }

func (n Arrow) String() string { return n.Tupleset + "->" + n.Relation }

func (n Union) String() string { return join(n.Children, " + ") }

func (n Intersection) String() string { return join(n.Children, " & ") }

func (n Exclusion) String() string { return group(n.Base) + " - " + group(n.Subtract) }

func Walk(n Node, fn func(Node) error) error {
	if err := fn(n); err != nil {
		return err
	}
	switch n := n.(type) {
	case Union:
		for _, c := range n.Children {
			if err := Walk(c, fn); err != nil {
				return err
			}
		}
	case Intersection:
		for _, c := range n.Children {
			if err := Walk(c, fn); err != nil {
				return err
			}
		}
	case Exclusion:
		if err := Walk(n.Base, fn); err != nil {
			return err
		}
		return Walk(n.Subtract, fn)
	}
	return nil
}

func join(children []Node, op string) string {
	parts := make([]string, len(children))
	for i, c := range children {
		parts[i] = group(c)
	}
	return strings.Join(parts, op)
}

func group(n Node) string {
	switch n.(type) {
	case Computed, Arrow:
		return n.String()
	}
	return "(" + n.String() + ")"
}
