package engine

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
	"github.com/c4ptlevi/margit/store"
)

func direct(name string, types ...string) model.Relation {
	return model.Relation{Name: name, AllowedTypes: types}
}

func computed(name, expr string) model.Relation {
	return model.Relation{Name: name, RelExpr: expr}
}

func ns(name string, rels ...model.Relation) model.Namespace {
	m := make(map[string]model.Relation, len(rels))
	for _, r := range rels {
		m[r.Name] = r
	}
	return model.Namespace{Name: name, Relations: m}
}

func ent(s string) model.Entity {
	nsName, id, _ := strings.Cut(s, ":")
	return model.Entity{Namespace: nsName, ID: id}
}

func tuple(obj, rel, sub string) model.RelationTuple {
	return model.RelationTuple{Object: ent(obj), Relation: rel, Subject: ent(sub)}
}

var newStore = func(t *testing.T) store.Store { return store.NewMemoryStore(nil) }

func newTestEngine(t *testing.T, cfg Config, log *logger.Logger) *Engine {
	t.Helper()
	ctx := context.Background()
	e := New(newStore(t), nil, cfg, log)
	schema := []model.Namespace{
		ns("user"),
		ns("group", direct("member", "user")),
		ns("folder",
			direct("parent", "folder"),
			direct("owner", "user"),
			direct("viewer_direct", "user"),
			direct("viewer_group", "group"),
			computed("viewer", "owner + viewer_direct + viewer_group->member + parent->viewer"),
		),
		ns("document",
			direct("parent", "folder"),
			direct("owner", "user"),
			direct("editor_direct", "user"),
			direct("banned", "user"),
			direct("auditor_direct", "user"),
			computed("editor", "owner + editor_direct"),
			computed("viewer", "(editor + parent->viewer) - banned"),
			computed("audit", "viewer & auditor_direct"),
		),
	}
	for _, n := range schema {
		if err := e.SaveNamespace(ctx, n); err != nil {
			t.Fatalf("SaveNamespace(%s): %v", n.Name, err)
		}
	}
	err := e.WriteTuples(ctx, []model.RelationTuple{
		tuple("group:eng", "member", "user:bob"),
		tuple("folder:root", "owner", "user:alice"),
		tuple("folder:root", "viewer_direct", "user:dave"),
		tuple("folder:sub", "parent", "folder:root"),
		tuple("folder:sub", "viewer_group", "group:eng"),
		tuple("document:readme", "parent", "folder:sub"),
		tuple("document:readme", "editor_direct", "user:carol"),
		tuple("document:readme", "banned", "user:dave"),
		tuple("document:readme", "auditor_direct", "user:alice"),
		tuple("document:secret", "parent", "folder:sub"),
		tuple("document:secret", "banned", "user:bob"),
		tuple("folder:loop1", "parent", "folder:loop2"),
		tuple("folder:loop2", "parent", "folder:loop1"),
		tuple("folder:loop1", "owner", "user:erin"),
	})
	if err != nil {
		t.Fatalf("WriteTuples: %v", err)
	}
	return e
}

func TestCheck(t *testing.T) {
	e := newTestEngine(t, Config{}, nil)
	ctx := context.Background()
	cases := []struct {
		obj, rel, sub string
		want          bool
	}{
		{"document:readme", "viewer", "user:alice", true},
		{"document:readme", "viewer", "user:bob", true},
		{"document:readme", "viewer", "user:carol", true},
		{"document:readme", "viewer", "user:dave", false},
		{"document:readme", "viewer", "user:erin", false},
		{"document:secret", "viewer", "user:bob", false},
		{"document:secret", "viewer", "user:alice", true},
		{"document:readme", "editor", "user:carol", true},
		{"document:readme", "editor", "user:bob", false},
		{"document:readme", "audit", "user:alice", true},
		{"document:readme", "audit", "user:carol", false},
		{"folder:sub", "viewer", "user:dave", true},
		{"folder:loop2", "viewer", "user:erin", true},
		{"folder:loop1", "viewer", "user:bob", false},
		{"group:eng", "member", "user:bob", true},
	}
	for _, c := range cases {
		got, err := e.Check(ctx, ent(c.obj), c.rel, ent(c.sub))
		if err != nil {
			t.Errorf("Check(%s#%s@%s): %v", c.obj, c.rel, c.sub, err)
			continue
		}
		if got != c.want {
			t.Errorf("Check(%s#%s@%s) = %v, want %v", c.obj, c.rel, c.sub, got, c.want)
		}
	}
}

func TestCheckErrors(t *testing.T) {
	e := newTestEngine(t, Config{}, nil)
	ctx := context.Background()
	cases := []struct {
		obj, rel, sub string
		want          error
	}{
		{"widget:1", "viewer", "user:alice", model.ErrUnknownNamespace},
		{"document:readme", "approver", "user:alice", model.ErrUnknownRelation},
		{"document:", "viewer", "user:alice", model.ErrEmptyID},
		{"document:readme", "bad-rel", "user:alice", model.ErrInvalidIdentifier},
	}
	for _, c := range cases {
		if _, err := e.Check(ctx, ent(c.obj), c.rel, ent(c.sub)); !errors.Is(err, c.want) {
			t.Errorf("Check(%s#%s@%s) err = %v, want %v", c.obj, c.rel, c.sub, err, c.want)
		}
	}
}

func TestCheckMaxDepth(t *testing.T) {
	e := newTestEngine(t, Config{MaxDepth: 2}, nil)
	_, err := e.Check(context.Background(), ent("document:readme"), "viewer", ent("user:alice"))
	if !errors.Is(err, model.ErrMaxDepthExceeded) {
		t.Fatalf("err = %v, want ErrMaxDepthExceeded", err)
	}
}

func TestCheckCancelled(t *testing.T) {
	e := newTestEngine(t, Config{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Check(ctx, ent("document:readme"), "viewer", ent("user:alice")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func names(es []model.Entity) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.String()
	}
	return out
}

func TestExpand(t *testing.T) {
	e := newTestEngine(t, Config{}, nil)
	ctx := context.Background()
	cases := []struct {
		obj, rel string
		want     []string
	}{
		{"document:readme", "viewer", []string{"user:alice", "user:bob", "user:carol"}},
		{"document:secret", "viewer", []string{"user:alice", "user:dave"}},
		{"document:readme", "audit", []string{"user:alice"}},
		{"folder:sub", "viewer", []string{"user:alice", "user:bob", "user:dave"}},
		{"folder:loop1", "viewer", []string{"user:erin"}},
		{"document:readme", "parent", []string{"folder:sub"}},
	}
	for _, c := range cases {
		got, err := e.Expand(ctx, ent(c.obj), c.rel)
		if err != nil {
			t.Errorf("Expand(%s#%s): %v", c.obj, c.rel, err)
			continue
		}
		if !slices.Equal(names(got), c.want) {
			t.Errorf("Expand(%s#%s) = %v, want %v", c.obj, c.rel, names(got), c.want)
		}
	}
}

func TestLookup(t *testing.T) {
	e := newTestEngine(t, Config{}, nil)
	ctx := context.Background()
	cases := []struct {
		sub, rel, namespace string
		want                []string
	}{
		{"user:bob", "viewer", "document", []string{"document:readme"}},
		{"user:alice", "viewer", "document", []string{"document:readme", "document:secret"}},
		{"user:dave", "viewer", "document", []string{"document:secret"}},
		{"user:alice", "viewer", "folder", []string{"folder:root", "folder:sub"}},
		{"user:erin", "viewer", "folder", []string{"folder:loop1", "folder:loop2"}},
		{"user:alice", "audit", "document", []string{"document:readme"}},
		{"user:zoe", "viewer", "document", nil},
	}
	for _, c := range cases {
		got, err := e.Lookup(ctx, ent(c.sub), c.rel, c.namespace)
		if err != nil {
			t.Errorf("Lookup(%s, %s, %s): %v", c.sub, c.rel, c.namespace, err)
			continue
		}
		if !slices.Equal(names(got), c.want) {
			t.Errorf("Lookup(%s, %s, %s) = %v, want %v", c.sub, c.rel, c.namespace, names(got), c.want)
		}
	}
	if _, err := e.Lookup(ctx, ent("user:bob"), "viewer", "widget"); !errors.Is(err, model.ErrUnknownNamespace) {
		t.Errorf("Lookup unknown namespace err = %v", err)
	}
}

func TestWriteValidation(t *testing.T) {
	e := newTestEngine(t, Config{}, nil)
	ctx := context.Background()
	cases := []struct {
		tuple model.RelationTuple
		want  error
	}{
		{tuple("document:x", "owner", "group:eng"), model.ErrSubjectTypeNotAllowed},
		{tuple("document:x", "viewer", "user:bob"), model.ErrRelationNotDirect},
		{tuple("document:x", "approver", "user:bob"), model.ErrUnknownRelation},
		{tuple("widget:x", "owner", "user:bob"), model.ErrUnknownNamespace},
	}
	for _, c := range cases {
		batch := []model.RelationTuple{tuple("document:x", "owner", "user:bob"), c.tuple}
		if err := e.WriteTuples(ctx, batch); !errors.Is(err, c.want) {
			t.Errorf("WriteTuples(%s) err = %v, want %v", c.tuple, err, c.want)
		}
	}
	if ok, _ := e.Check(ctx, ent("document:x"), "owner", ent("user:bob")); ok {
		t.Fatal("rejected batch was partially written")
	}

	if err := e.SaveNamespace(ctx, ns("project", direct("owner", "team"))); !errors.Is(err, model.ErrUnknownNamespace) {
		t.Fatalf("SaveNamespace err = %v, want ErrUnknownNamespace", err)
	}

	if err := e.DeleteTuples(ctx, []model.RelationTuple{tuple("document:readme", "banned", "user:dave")}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := e.Check(ctx, ent("document:readme"), "viewer", ent("user:dave")); !ok {
		t.Fatal("dave should view readme after ban removed")
	}
}

func TestLogsCarryTrace(t *testing.T) {
	var buf bytes.Buffer
	e := newTestEngine(t, Config{}, logger.New(&buf, logger.LevelDebug))
	buf.Reset()
	ctx := logger.WithTraceID(context.Background(), "req-7")
	if _, err := e.Check(ctx, ent("document:readme"), "viewer", ent("user:bob")); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "msg=\"check done\"") || !strings.Contains(out, "trace=req-7") || !strings.Contains(out, "allowed=true") {
		t.Fatalf("logs = %q", out)
	}
}
