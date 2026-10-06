package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/c4ptlevi/margit/model"
)

func runStoreTests(t *testing.T, newStore func(t *testing.T) Store) {
	ctx := context.Background()
	user := func(id string) model.Entity { return model.Entity{Namespace: "user", ID: id} }
	doc := func(id string) model.Entity { return model.Entity{Namespace: "document", ID: id} }
	documentNS := model.Namespace{
		Name: "document",
		Relations: map[string]model.Relation{
			"viewer":   {Name: "viewer", AllowedTypes: []string{"user"}},
			"can_view": {Name: "can_view", RelExpr: "viewer"},
		},
	}

	t.Run("namespaces", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.GetNamespace(ctx, "document"); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("GetNamespace missing: err = %v, want ErrNotFound", err)
		}
		if err := s.SaveNamespace(ctx, documentNS); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveNamespace(ctx, model.Namespace{Name: "user", Relations: map[string]model.Relation{}}); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetNamespace(ctx, "document")
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "document" || len(got.Relations) != 2 || got.Relations["can_view"].RelExpr != "viewer" ||
			!slices.Equal(got.Relations["viewer"].AllowedTypes, []string{"user"}) {
			t.Fatalf("GetNamespace = %#v", got)
		}

		list, err := s.ListNamespaces(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].Name != "document" || list[1].Name != "user" {
			t.Fatalf("ListNamespaces = %#v", list)
		}

		for _, c := range []struct {
			ns, rel string
			want    bool
		}{
			{"document", "viewer", true},
			{"document", "editor", false},
			{"folder", "viewer", false},
		} {
			ok, err := s.RelationExists(ctx, c.ns, c.rel)
			if err != nil || ok != c.want {
				t.Fatalf("RelationExists(%s, %s) = %v, %v; want %v", c.ns, c.rel, ok, err, c.want)
			}
		}

		updated := documentNS
		updated.Relations = map[string]model.Relation{"owner": {Name: "owner", AllowedTypes: []string{"user"}}}
		if err := s.SaveNamespace(ctx, updated); err != nil {
			t.Fatal(err)
		}
		if ok, _ := s.RelationExists(ctx, "document", "viewer"); ok {
			t.Fatal("SaveNamespace did not replace relations")
		}

		if err := s.DeleteNamespace(ctx, "document"); err != nil {
			t.Fatal(err)
		}
		if ok, err := s.NamespaceExists(ctx, "document"); ok || err != nil {
			t.Fatalf("NamespaceExists after delete = %v, %v", ok, err)
		}
		if err := s.DeleteNamespace(ctx, "document"); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("DeleteNamespace missing: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("tuples", func(t *testing.T) {
		s := newStore(t)
		tuples := []model.RelationTuple{
			{Object: doc("1"), Relation: "viewer", Subject: user("alice")},
			{Object: doc("1"), Relation: "viewer", Subject: user("bob")},
			{Object: doc("2"), Relation: "viewer", Subject: user("alice")},
			{Object: doc("1"), Relation: "owner", Subject: user("carol")},
		}
		if err := s.WriteTuples(ctx, tuples); err != nil {
			t.Fatal(err)
		}
		if err := s.WriteTuples(ctx, tuples[:1]); err != nil {
			t.Fatalf("duplicate write: %v", err)
		}

		assertTuples(t, s.ReadTuples, doc("1"), "viewer", "document:1#viewer@user:alice", "document:1#viewer@user:bob")
		assertTuples(t, s.ReadTuples, doc("1"), "owner", "document:1#owner@user:carol")
		assertTuples(t, s.ReadTuples, doc("3"), "viewer")
		assertTuples(t, s.ReadTuplesBySubject, user("alice"), "viewer", "document:1#viewer@user:alice", "document:2#viewer@user:alice")
		assertTuples(t, s.ReadTuplesBySubject, user("carol"), "viewer")

		if err := s.DeleteTuples(ctx, []model.RelationTuple{tuples[0], {Object: doc("9"), Relation: "viewer", Subject: user("x")}}); err != nil {
			t.Fatal(err)
		}
		assertTuples(t, s.ReadTuples, doc("1"), "viewer", "document:1#viewer@user:bob")
		assertTuples(t, s.ReadTuplesBySubject, user("alice"), "viewer", "document:2#viewer@user:alice")

		if err := s.WriteTuples(ctx, nil); err != nil {
			t.Fatalf("empty write: %v", err)
		}

		var all []string
		err := s.ForEachTuple(ctx, func(tp model.RelationTuple) error {
			all = append(all, tp.String())
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(all)
		want := []string{"document:1#owner@user:carol", "document:1#viewer@user:bob", "document:2#viewer@user:alice"}
		if !slices.Equal(all, want) {
			t.Fatalf("ForEachTuple = %v, want %v", all, want)
		}

		stop := errors.New("stop")
		if err := s.ForEachTuple(ctx, func(model.RelationTuple) error { return stop }); !errors.Is(err, stop) {
			t.Fatalf("ForEachTuple stop: err = %v", err)
		}
	})

	t.Run("namespace isolation", func(t *testing.T) {
		s := newStore(t)
		ns := model.Namespace{Name: "document", Relations: map[string]model.Relation{
			"viewer": {Name: "viewer", AllowedTypes: []string{"user"}},
		}}
		if err := s.SaveNamespace(ctx, ns); err != nil {
			t.Fatal(err)
		}
		ns.Relations["viewer"].AllowedTypes[0] = "group"
		ns.Relations["editor"] = model.Relation{Name: "editor", AllowedTypes: []string{"user"}}
		got, err := s.GetNamespace(ctx, "document")
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Relations) != 1 || got.Relations["viewer"].AllowedTypes[0] != "user" {
			t.Fatalf("stored namespace was mutated by caller: %#v", got)
		}
	})
}

type readFunc func(context.Context, model.Entity, string) ([]model.RelationTuple, error)

func assertTuples(t *testing.T, read readFunc, e model.Entity, relation string, want ...string) {
	t.Helper()
	tuples, err := read(context.Background(), e, relation)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(tuples))
	for i, tp := range tuples {
		got[i] = tp.String()
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("read(%s, %s) = [%s], want [%s]", e, relation, strings.Join(got, ", "), strings.Join(want, ", "))
	}
}
