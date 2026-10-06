package store

import (
	"testing"

	"github.com/c4ptlevi/margit/model"
)

func TestTupleFilter(t *testing.T) {
	doc := model.Entity{Namespace: "document", ID: "1"}
	alice := model.Entity{Namespace: "user", ID: "alice"}

	var disabled *tupleFilter
	disabled.addTuple(model.RelationTuple{Object: doc, Relation: "viewer", Subject: alice})
	if !disabled.mayHaveNamespace("x") || !disabled.mayHaveObject(doc, "x") {
		t.Fatal("disabled filter must always answer maybe")
	}
	if newTupleFilter(0, 0.01) != nil {
		t.Fatal("expected 0 should disable the filter")
	}

	f := newTupleFilter(1_000, 0.001)
	f.addNamespace(model.Namespace{Name: "document", Relations: map[string]model.Relation{"viewer": {Name: "viewer"}}})
	f.addTuple(model.RelationTuple{Object: doc, Relation: "viewer", Subject: alice})

	if !f.mayHaveNamespace("document") || !f.mayHaveRelation("document", "viewer") ||
		!f.mayHaveObject(doc, "viewer") || !f.mayHaveSubject(alice, "viewer") {
		t.Fatal("false negative")
	}
	if f.mayHaveNamespace("folder") || f.mayHaveRelation("document", "editor") ||
		f.mayHaveObject(alice, "viewer") || f.mayHaveSubject(doc, "viewer") {
		t.Fatal("unexpected hit for absent key")
	}
}
