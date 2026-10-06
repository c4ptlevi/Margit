package store

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/c4ptlevi/margit/model"
)

func TestPostgresStore(t *testing.T) {
	dsn := os.Getenv("MARGIT_PG_DSN")
	if dsn == "" {
		t.Skip("MARGIT_PG_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	for name, cfg := range map[string]PostgresConfig{
		"bloom":    {BloomExpected: 10_000, BloomFPRate: 0.01},
		"no bloom": {},
	} {
		t.Run(name, func(t *testing.T) {
			runStoreTests(t, func(t *testing.T) Store {
				if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS namespaces, relation_tuples`); err != nil {
					t.Fatal(err)
				}
				s, err := NewPostgresStore(ctx, pool, cfg, nil)
				if err != nil {
					t.Fatal(err)
				}
				return s
			})
		})
	}

	t.Run("bloom loads existing data", func(t *testing.T) {
		cfg := PostgresConfig{BloomExpected: 10_000, BloomFPRate: 0.01}
		if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS namespaces, relation_tuples`); err != nil {
			t.Fatal(err)
		}
		first, err := NewPostgresStore(ctx, pool, cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		doc := model.Entity{Namespace: "document", ID: "1"}
		alice := model.Entity{Namespace: "user", ID: "alice"}
		ns := model.Namespace{Name: "document", Relations: map[string]model.Relation{
			"viewer": {Name: "viewer", AllowedTypes: []string{"user"}},
		}}
		if err := first.SaveNamespace(ctx, ns); err != nil {
			t.Fatal(err)
		}
		if err := first.WriteTuples(ctx, []model.RelationTuple{{Object: doc, Relation: "viewer", Subject: alice}}); err != nil {
			t.Fatal(err)
		}

		second, err := NewPostgresStore(ctx, pool, cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := second.RelationExists(ctx, "document", "viewer"); err != nil || !ok {
			t.Fatalf("RelationExists = %v, %v", ok, err)
		}
		if got, err := second.ReadTuples(ctx, doc, "viewer"); err != nil || len(got) != 1 {
			t.Fatalf("ReadTuples = %v, %v", got, err)
		}
		if got, err := second.ReadTuplesBySubject(ctx, alice, "viewer"); err != nil || len(got) != 1 {
			t.Fatalf("ReadTuplesBySubject = %v, %v", got, err)
		}
	})
}
