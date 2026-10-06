package model

import (
	"errors"
	"reflect"
	"testing"
)

func direct(name string, types ...string) Relation {
	return Relation{Name: name, AllowedTypes: types}
}

func computed(name, expr string) Relation {
	return Relation{Name: name, RelExpr: expr}
}

func ns(name string, rels ...Relation) Namespace {
	m := make(map[string]Relation, len(rels))
	for _, r := range rels {
		m[r.Name] = r
	}
	return Namespace{Name: name, Relations: m}
}

func docSchema() []Namespace {
	return []Namespace{
		ns("user"),
		ns("group", direct("member", "user")),
		ns("folder", direct("viewer", "user"), computed("can_view", "viewer")),
		ns("document",
			direct("parent", "folder"),
			direct("owner", "user"),
			direct("viewer", "user"),
			direct("viewer_group", "group"),
			direct("banned", "user"),
			computed("can_view", "(owner + viewer + viewer_group->member + parent->can_view) - banned"),
		),
	}
}

func TestErrorText(t *testing.T) {
	if ErrNotFound.Error() != "not found" {
		t.Fatalf("ErrNotFound.Error() = %q", ErrNotFound.Error())
	}
	if Error(999).Error() != "unknown error" {
		t.Fatalf("Error(999).Error() = %q", Error(999).Error())
	}
}

func TestEntityValidate(t *testing.T) {
	tests := []struct {
		e    Entity
		want error
	}{
		{Entity{"user", "alice"}, nil},
		{Entity{"", "alice"}, ErrInvalidIdentifier},
		{Entity{"us er", "alice"}, ErrInvalidIdentifier},
		{Entity{"user", ""}, ErrEmptyID},
	}
	for _, tt := range tests {
		if err := tt.e.Validate(); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
			t.Errorf("%#v.Validate() = %v, want %v", tt.e, err, tt.want)
		}
	}
}

func TestRelationValidate(t *testing.T) {
	tests := []struct {
		name string
		r    Relation
		want error
	}{
		{"direct", direct("viewer", "user", "group"), nil},
		{"computed", computed("can_view", "owner + viewer"), nil},
		{"bad name", direct("1viewer", "user"), ErrInvalidIdentifier},
		{"neither", Relation{Name: "viewer"}, ErrInvalidRelationKind},
		{"both", Relation{Name: "viewer", AllowedTypes: []string{"user"}, RelExpr: "owner"}, ErrInvalidRelationKind},
		{"bad type", direct("viewer", "us-er"), ErrInvalidIdentifier},
		{"dup type", direct("viewer", "user", "user"), ErrDuplicateType},
		{"bad expr", computed("can_view", "owner +"), ErrInvalidExpression},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.r.Validate()
			if tt.want == nil && err != nil || !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func getter(nss ...Namespace) func(string) (Namespace, error) {
	m := make(map[string]Namespace, len(nss))
	for _, n := range nss {
		m[n.Name] = n
	}
	return func(name string) (Namespace, error) {
		n, ok := m[name]
		if !ok {
			return Namespace{}, ErrNotFound
		}
		return n, nil
	}
}

func TestNamespaceValidate(t *testing.T) {
	schema := getter(docSchema()...)
	tests := []struct {
		name string
		ns   Namespace
		get  func(string) (Namespace, error)
		want error
	}{
		{"empty", ns("user"), schema, nil},
		{"valid", docSchema()[3], schema, nil},
		{"bad name", ns("my doc"), schema, ErrInvalidIdentifier},
		{"key mismatch", Namespace{Name: "doc", Relations: map[string]Relation{"a": direct("b", "user")}}, schema, ErrRelationNameMismatch},
		{"invalid relation", ns("doc", Relation{Name: "viewer"}), schema, ErrInvalidRelationKind},
		{"unknown computed", ns("doc", computed("can_view", "viewer")), schema, ErrUnknownRelation},
		{"unknown tupleset", ns("doc", computed("can_view", "parent->viewer")), schema, ErrUnknownRelation},
		{
			"computed tupleset",
			ns("doc", direct("owner", "user"), computed("parent", "owner"), computed("can_view", "parent->viewer")),
			schema,
			ErrInvalidTupleset,
		},
		{"self cycle", ns("doc", computed("a", "a")), schema, ErrCyclicRelation},
		{"cycle", ns("doc", computed("a", "b"), computed("b", "c + a"), computed("c", "b")), schema, ErrCyclicRelation},
		{"arrow is not a cycle", ns("folder", direct("parent", "folder"), computed("can_view", "parent->can_view")), schema, nil},
		{"unknown allowed type", ns("doc", direct("viewer", "nobody")), schema, ErrUnknownNamespace},
		{
			"arrow target missing",
			ns("doc", direct("parent", "folder"), computed("can_view", "parent->viewer")),
			getter(ns("folder")),
			ErrUnknownRelation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ns.Validate(tt.get)
			if tt.want == nil && err != nil || !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNamespaceValidateFetchesOnlyReferenced(t *testing.T) {
	schema := getter(docSchema()...)
	fetched := map[string]int{}
	get := func(name string) (Namespace, error) {
		fetched[name]++
		return schema(name)
	}
	if err := docSchema()[3].Validate(get); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"user": 1, "group": 1, "folder": 1}
	if !reflect.DeepEqual(fetched, want) {
		t.Fatalf("fetched = %v, want %v", fetched, want)
	}
}

func TestNamespaceValidateGetError(t *testing.T) {
	boom := errors.New("boom")
	err := ns("doc", direct("viewer", "user")).Validate(func(string) (Namespace, error) { return Namespace{}, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("Validate() = %v, want %v", err, boom)
	}
}
func TestTupleValidateAgainst(t *testing.T) {
	doc := docSchema()[3]
	alice := Entity{"user", "alice"}
	d42 := Entity{"document", "42"}
	tests := []struct {
		name string
		t    RelationTuple
		want error
	}{
		{"valid", RelationTuple{d42, "viewer", alice}, nil},
		{"valid group", RelationTuple{d42, "viewer_group", Entity{"group", "eng"}}, nil},
		{"bad object", RelationTuple{Entity{"document", ""}, "viewer", alice}, ErrEmptyID},
		{"bad subject", RelationTuple{d42, "viewer", Entity{"", "alice"}}, ErrInvalidIdentifier},
		{"bad relation", RelationTuple{d42, "", alice}, ErrInvalidIdentifier},
		{"wrong namespace", RelationTuple{Entity{"folder", "1"}, "viewer", alice}, ErrNamespaceMismatch},
		{"unknown relation", RelationTuple{d42, "editor", alice}, ErrUnknownRelation},
		{"computed relation", RelationTuple{d42, "can_view", alice}, ErrRelationNotDirect},
		{"type not allowed", RelationTuple{d42, "viewer", Entity{"group", "eng"}}, ErrSubjectTypeNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.t.ValidateAgainst(doc)
			if tt.want == nil && err != nil || !errors.Is(err, tt.want) {
				t.Fatalf("ValidateAgainst() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestTupleString(t *testing.T) {
	tp := RelationTuple{Entity{"document", "42"}, "viewer", Entity{"user", "bob"}}
	if got := tp.String(); got != "document:42#viewer@user:bob" {
		t.Fatalf("String() = %q", got)
	}
}
