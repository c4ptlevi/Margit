package engine

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/c4ptlevi/margit/store"
)

func TestEnginePostgres(t *testing.T) {
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

	prev := newStore
	t.Cleanup(func() { newStore = prev })
	newStore = func(t *testing.T) store.Store {
		if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS namespaces, relation_tuples`); err != nil {
			t.Fatal(err)
		}
		s, err := store.NewPostgresStore(ctx, pool, store.PostgresConfig{BloomExpected: 10_000, BloomFPRate: 0.01}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	t.Run("check", TestCheck)
	t.Run("check errors", TestCheckErrors)
	t.Run("expand", TestExpand)
	t.Run("lookup", TestLookup)
	t.Run("write validation", TestWriteValidation)
	t.Run("namespaces", TestNamespaces)
}
