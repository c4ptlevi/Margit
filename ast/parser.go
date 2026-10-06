package ast

import "fmt"

type parser struct {
	toks []token
	i    int
}

func Parse(src string) (Node, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tokEOF {
		return nil, p.errorf(t, "unexpected %s", t)
	}
	return n, nil
}

func (p *parser) peek() token {
	return p.toks[p.i]
}

func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tokEOF {
		p.i++
	}
	return t
}

func (p *parser) errorf(t token, format string, args ...any) error {
	return &SyntaxError{Pos: t.pos, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) parseExpr() (Node, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peek()
		if op.kind != tokPlus && op.kind != tokAmp && op.kind != tokMinus {
			return left, nil
		}
		p.next()
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		left = combine(op.kind, left, right)
	}
}

func combine(op tokenKind, left, right Node) Node {
	switch op {
	case tokPlus:
		if u, ok := left.(Union); ok {
			return Union{Children: append(u.Children, right)}
		}
		return Union{Children: []Node{left, right}}
	case tokAmp:
		if in, ok := left.(Intersection); ok {
			return Intersection{Children: append(in.Children, right)}
		}
		return Intersection{Children: []Node{left, right}}
	default:
		return Exclusion{Base: left, Subtract: right}
	}
}

func (p *parser) parseTerm() (Node, error) {
	t := p.next()
	switch t.kind {
	case tokIdent:
		if p.peek().kind != tokArrow {
			return Computed{Relation: t.text}, nil
		}
		p.next()
		rel := p.next()
		if rel.kind != tokIdent {
			return nil, p.errorf(rel, "expected relation name after \"->\", got %s", rel)
		}
		return Arrow{Tupleset: t.text, Relation: rel.text}, nil
	case tokLParen:
		n, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != tokRParen {
			return nil, p.errorf(c, "expected \")\", got %s", c)
		}
		return n, nil
	default:
		return nil, p.errorf(t, "expected relation name or \"(\", got %s", t)
	}
}
