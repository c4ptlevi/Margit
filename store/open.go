package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/c4ptlevi/margit/logger"
)

type Kind string

const (
	KindMemory   Kind = "memory"
	KindPostgres Kind = "postgres"
)

type Config struct {
	Type     Kind           `json:"type"`
	Postgres PostgresConfig `json:"postgres"`
}

func Open(ctx context.Context, cfg Config, log *logger.Logger) (Store, func(), error) {
	switch cfg.Type {
	case KindMemory:
		log.Info(ctx, "tag_q5a97t", "store opened", "type", cfg.Type)
		return NewMemoryStore(log), func() {}, nil
	case KindPostgres:
		pool, err := pgxpool.New(ctx, cfg.Postgres.DSN)
		if err != nil {
			log.Error(ctx, "tag_49xr1c", "postgres pool init failed", "err", err)
			return nil, nil, err
		}
		s, err := NewPostgresStore(ctx, pool, cfg.Postgres, log)
		if err != nil {
			pool.Close()
			return nil, nil, err
		}
		log.Info(ctx, "tag_nes74z", "store opened", "type", cfg.Type)
		return s, pool.Close, nil
	default:
		return nil, nil, fmt.Errorf("unknown store type %q", cfg.Type)
	}
}
