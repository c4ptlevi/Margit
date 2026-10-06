package store

import (
	"github.com/c4ptlevi/margit/bloom"
	"github.com/c4ptlevi/margit/model"
)

type tupleFilter struct {
	f *bloom.Filter
}

func newTupleFilter(expected uint64, fpRate float64) *tupleFilter {
	if expected == 0 {
		return nil
	}
	return &tupleFilter{f: bloom.New(expected, fpRate)}
}

func (t *tupleFilter) addNamespace(ns model.Namespace) {
	if t == nil {
		return
	}
	t.f.Add(nsKey(ns.Name))
	for name := range ns.Relations {
		t.f.Add(relKey(ns.Name, name))
	}
}

func (t *tupleFilter) addTuple(tp model.RelationTuple) {
	if t == nil {
		return
	}
	t.f.Add(edgeKeyString("o", tp.Object, tp.Relation))
	t.f.Add(edgeKeyString("s", tp.Subject, tp.Relation))
}

func (t *tupleFilter) mayHaveNamespace(name string) bool {
	return t == nil || t.f.Test(nsKey(name))
}

func (t *tupleFilter) mayHaveRelation(namespace, relation string) bool {
	return t == nil || t.f.Test(relKey(namespace, relation))
}

func (t *tupleFilter) mayHaveObject(obj model.Entity, relation string) bool {
	return t == nil || t.f.Test(edgeKeyString("o", obj, relation))
}

func (t *tupleFilter) mayHaveSubject(sub model.Entity, relation string) bool {
	return t == nil || t.f.Test(edgeKeyString("s", sub, relation))
}

func nsKey(name string) string {
	return "n\x00" + name
}

func relKey(namespace, relation string) string {
	return "r\x00" + namespace + "\x00" + relation
}

func edgeKeyString(kind string, e model.Entity, relation string) string {
	return kind + "\x00" + e.Namespace + "\x00" + e.ID + "\x00" + relation
}
