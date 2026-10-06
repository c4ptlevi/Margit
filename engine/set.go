package engine

import (
	"maps"
	"slices"
	"strings"

	"github.com/c4ptlevi/margit/model"
)

type entitySet map[model.Entity]struct{}

func (s entitySet) add(e model.Entity) { s[e] = struct{}{} }

func (s entitySet) has(e model.Entity) bool {
	_, ok := s[e]
	return ok
}

func (s entitySet) addAll(other entitySet) {
	for e := range other {
		s.add(e)
	}
}

func (s entitySet) retain(other entitySet) {
	for e := range s {
		if !other.has(e) {
			delete(s, e)
		}
	}
}

func (s entitySet) removeAll(other entitySet) {
	for e := range other {
		delete(s, e)
	}
}

func (s entitySet) clone() entitySet {
	out := make(entitySet, len(s))
	maps.Copy(out, s)
	return out
}

func (s entitySet) sorted() []model.Entity {
	out := slices.Collect(maps.Keys(s))
	slices.SortFunc(out, func(a, b model.Entity) int {
		return strings.Compare(a.String(), b.String())
	})
	return out
}
