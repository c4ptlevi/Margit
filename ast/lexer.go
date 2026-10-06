package ast

import "fmt"

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokPlus
	tokAmp
	tokMinus
	tokArrow
	tokLParen
	tokRParen
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

func (t token) String() string {
	if t.kind == tokEOF {
		return "end of input"
	}
	return fmt.Sprintf("%q", t.text)
}

type SyntaxError struct {
	Pos int
	Msg string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("ast: position %d: %s", e.Pos, e.Msg)
}

func lex(src string) ([]token, error) {
	var toks []token
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '+':
			toks = append(toks, token{tokPlus, "+", i})
			i++
		case c == '&':
			toks = append(toks, token{tokAmp, "&", i})
			i++
		case c == '(':
			toks = append(toks, token{tokLParen, "(", i})
			i++
		case c == ')':
			toks = append(toks, token{tokRParen, ")", i})
			i++
		case c == '-':
			if i+1 < len(src) && src[i+1] == '>' {
				toks = append(toks, token{tokArrow, "->", i})
				i += 2
			} else {
				toks = append(toks, token{tokMinus, "-", i})
				i++
			}
		case isIdentStart(c):
			start := i
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			toks = append(toks, token{tokIdent, src[start:i], start})
		default:
			return nil, &SyntaxError{Pos: i, Msg: fmt.Sprintf("unexpected character %q", c)}
		}
	}
	return append(toks, token{tokEOF, "", len(src)}), nil
}

func IsIdent(s string) bool {
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentPart(s[i]) {
			return false
		}
	}
	return true
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
