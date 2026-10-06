package ast

import (
	"errors"
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		src  string
		want Node
	}{
		{"owner", Computed{"owner"}},
		{"parent->viewer", Arrow{"parent", "viewer"}},
		{"  parent -> viewer  ", Arrow{"parent", "viewer"}},
		{"viewer + owner", Union{[]Node{Computed{"viewer"}, Computed{"owner"}}}},
		{"a + b + c", Union{[]Node{Computed{"a"}, Computed{"b"}, Computed{"c"}}}},
		{"a & b & c", Intersection{[]Node{Computed{"a"}, Computed{"b"}, Computed{"c"}}}},
		{"viewer - banned", Exclusion{Computed{"viewer"}, Computed{"banned"}}},
		{
			"viewer + editor - banned",
			Exclusion{Union{[]Node{Computed{"viewer"}, Computed{"editor"}}}, Computed{"banned"}},
		},
		{
			"viewer + (editor - banned)",
			Union{[]Node{Computed{"viewer"}, Exclusion{Computed{"editor"}, Computed{"banned"}}}},
		},
		{
			"owner + viewer + parent->can_view",
			Union{[]Node{Computed{"owner"}, Computed{"viewer"}, Arrow{"parent", "can_view"}}},
		},
		{
			"(a + b) & c",
			Intersection{[]Node{Union{[]Node{Computed{"a"}, Computed{"b"}}}, Computed{"c"}}},
		},
		{"((owner))", Computed{"owner"}},
		{"can_view_2", Computed{"can_view_2"}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got, err := Parse(tt.src)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.src, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Parse(%q) = %#v, want %#v", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		src string
		pos int
	}{
		{"", 0},
		{"   ", 3},
		{"owner +", 7},
		{"+ owner", 0},
		{"parent->", 8},
		{"parent->(viewer)", 8},
		{"(owner", 6},
		{"owner)", 5},
		{"owner viewer", 6},
		{"owner | viewer", 6},
		{"1owner", 0},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, err := Parse(tt.src)
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("Parse(%q) error = %v, want *SyntaxError", tt.src, err)
			}
			if se.Pos != tt.pos {
				t.Fatalf("Parse(%q) error pos = %d, want %d (%v)", tt.src, se.Pos, tt.pos, err)
			}
		})
	}
}

func TestStringRoundTrip(t *testing.T) {
	srcs := []string{
		"owner",
		"parent->viewer",
		"owner + viewer + parent->can_view",
		"(viewer + editor) - banned",
		"viewer + (editor - banned)",
		"(a + b) & (c - d)",
		"(a - b) - c",
	}
	for _, src := range srcs {
		t.Run(src, func(t *testing.T) {
			n, err := Parse(src)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", src, err)
			}
			if got := n.String(); got != src {
				t.Fatalf("String() = %q, want %q", got, src)
			}
			again, err := Parse(n.String())
			if err != nil {
				t.Fatalf("re-Parse(%q) error: %v", n.String(), err)
			}
			if !reflect.DeepEqual(again, n) {
				t.Fatalf("round trip mismatch: %#v vs %#v", again, n)
			}
		})
	}
}
