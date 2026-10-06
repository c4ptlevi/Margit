package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
)

var _ Store = (*PostgresStore)(nil)

const postgresSchema = `
CREATE TABLE IF NOT EXISTS namespaces (
	name      TEXT PRIMARY KEY,
	relations JSONB NOT NULL
);

CREATE TABLE IF NOT EXISTS relation_tuples (
	object_ns  TEXT NOT NULL,
	object_id  TEXT NOT NULL,
	relation   TEXT NOT NULL,
	subject_ns TEXT NOT NULL,
	subject_id TEXT NOT NULL,
	PRIMARY KEY (object_ns, object_id, relation, subject_ns, subject_id)
);

CREATE INDEX IF NOT EXISTS relation_tuples_subject_idx
	ON relation_tuples (subject_ns, subject_id, relation);
`

type PostgresConfig struct {
	DSN           string  `json:"dsn"`
	BloomExpected uint64  `json:"bloom_expected"`
	BloomFPRate   float64 `json:"bloom_fp_rate"`
}

type PostgresStore struct {
	pool   *pgxpool.Pool
	filter *tupleFilter
	log    *logger.Logger
}

func NewPostgresStore(ctx context.Context, pool *pgxpool.Pool, cfg PostgresConfig, log *logger.Logger) (*PostgresStore, error) {
	s := &PostgresStore{pool: pool, log: log}
	pc := pool.Config()
	log.Info(ctx, "tag_bwh5kg", "postgres pool configured", "host", pc.ConnConfig.Host, "database", pc.ConnConfig.Database,
		"max_conns", pc.MaxConns, "min_conns", pc.MinConns, "max_conn_lifetime", pc.MaxConnLifetime)
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	filter, err := s.loadFilter(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s.filter = filter
	return s, nil
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	start := time.Now()
	if _, err := s.pool.Exec(ctx, postgresSchema); err != nil {
		return s.fail(ctx, "tag_zl0zvm", "migrate", err)
	}
	s.log.Info(ctx, "tag_4edgsz", "postgres migration done", "took", time.Since(start))
	return nil
}

func (s *PostgresStore) loadFilter(ctx context.Context, cfg PostgresConfig) (*tupleFilter, error) {
	filter := newTupleFilter(cfg.BloomExpected, cfg.BloomFPRate)
	if filter == nil {
		s.log.Info(ctx, "tag_who9rx", "bloom filter disabled")
		return nil, nil
	}
	start := time.Now()
	namespaces, err := s.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	s.log.Info(ctx, "tag_0245pq", "bloom filter loading", "expected", cfg.BloomExpected, "fp_rate", cfg.BloomFPRate, "namespaces", len(namespaces))
	for _, ns := range namespaces {
		filter.addNamespace(ns)
	}
	tuples := 0
	err = s.ForEachTuple(ctx, func(t model.RelationTuple) error {
		filter.addTuple(t)
		tuples++
		if tuples%1_000_000 == 0 {
			s.log.Info(ctx, "tag_59brrm", "bloom filter loading", "tuples", tuples, "took", time.Since(start))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if uint64(2*tuples+len(namespaces)) > cfg.BloomExpected {
		s.log.Warn(ctx, "tag_oacwkz", "bloom filter over capacity, false positive rate will rise", "expected", cfg.BloomExpected, "tuples", tuples)
	}
	s.log.Info(ctx, "tag_bz25lq", "bloom filter loaded", "namespaces", len(namespaces), "tuples", tuples, "took", time.Since(start))
	return filter, nil
}

func (s *PostgresStore) fail(ctx context.Context, tag, op string, err error) error {
	switch {
	case err == nil, errors.Is(err, model.ErrNotFound):
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		s.log.Warn(ctx, tag, "postgres op cancelled", "op", op, "err", err)
	default:
		s.log.Error(ctx, tag, "postgres op failed", "op", op, "err", err)
	}
	return err
}

func (s *PostgresStore) SaveNamespace(ctx context.Context, ns model.Namespace) error {
	rels, err := json.Marshal(ns.Relations)
	if err != nil {
		return s.fail(ctx, "tag_mk2wvs", "SaveNamespace", err)
	}
	s.filter.addNamespace(ns)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO namespaces (name, relations) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET relations = EXCLUDED.relations`,
		ns.Name, rels)
	if err != nil {
		return s.fail(ctx, "tag_cve2s0", "SaveNamespace", err)
	}
	s.log.Debug(ctx, "tag_ybsp5d", "namespace saved", "namespace", ns.Name, "relations", len(ns.Relations))
	return nil
}

func (s *PostgresStore) GetNamespace(ctx context.Context, name string) (model.Namespace, error) {
	if !s.filter.mayHaveNamespace(name) {
		s.log.Debug(ctx, "tag_7q4ke5", "bloom miss", "op", "GetNamespace", "namespace", name)
		return model.Namespace{}, fmt.Errorf("%w: namespace %q", model.ErrNotFound, name)
	}
	var rels []byte
	err := s.pool.QueryRow(ctx, `SELECT relations FROM namespaces WHERE name = $1`, name).Scan(&rels)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Namespace{}, fmt.Errorf("%w: namespace %q", model.ErrNotFound, name)
	}
	if err != nil {
		return model.Namespace{}, s.fail(ctx, "tag_3253rc", "GetNamespace", err)
	}
	ns, err := decodeNamespace(name, rels)
	return ns, s.fail(ctx, "tag_a52uxm", "GetNamespace", err)
}

func (s *PostgresStore) ListNamespaces(ctx context.Context) ([]model.Namespace, error) {
	rows, err := s.pool.Query(ctx, `SELECT name, relations FROM namespaces ORDER BY name`)
	if err != nil {
		return nil, s.fail(ctx, "tag_rwh9ma", "ListNamespaces", err)
	}
	defer rows.Close()
	var out []model.Namespace
	for rows.Next() {
		var (
			name string
			rels []byte
		)
		if err := rows.Scan(&name, &rels); err != nil {
			return nil, s.fail(ctx, "tag_9kn0fu", "ListNamespaces", err)
		}
		ns, err := decodeNamespace(name, rels)
		if err != nil {
			return nil, s.fail(ctx, "tag_mu0ogs", "ListNamespaces", err)
		}
		out = append(out, ns)
	}
	return out, s.fail(ctx, "tag_fvk7u9", "ListNamespaces", rows.Err())
}

func (s *PostgresStore) DeleteNamespace(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM namespaces WHERE name = $1`, name)
	if err != nil {
		return s.fail(ctx, "tag_waini0", "DeleteNamespace", err)
	}
	if tag.RowsAffected() == 0 {
		s.log.Debug(ctx, "tag_vhmjyr", "namespace delete found nothing", "namespace", name)
		return fmt.Errorf("%w: namespace %q", model.ErrNotFound, name)
	}
	s.log.Debug(ctx, "tag_0gc1v4", "namespace deleted", "namespace", name)
	return nil
}

func (s *PostgresStore) NamespaceExists(ctx context.Context, name string) (bool, error) {
	if !s.filter.mayHaveNamespace(name) {
		s.log.Debug(ctx, "tag_fmx4lx", "bloom miss", "op", "NamespaceExists", "namespace", name)
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM namespaces WHERE name = $1)`, name).Scan(&ok)
	return ok, s.fail(ctx, "tag_2hefmn", "NamespaceExists", err)
}

func (s *PostgresStore) RelationExists(ctx context.Context, namespace string, relation string) (bool, error) {
	if !s.filter.mayHaveRelation(namespace, relation) {
		s.log.Debug(ctx, "tag_0j86nw", "bloom miss", "op", "RelationExists", "namespace", namespace, "relation", relation)
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM namespaces WHERE name = $1 AND relations ? $2)`,
		namespace, relation).Scan(&ok)
	return ok, s.fail(ctx, "tag_gjpj89", "RelationExists", err)
}

func (s *PostgresStore) WriteTuples(ctx context.Context, tuples []model.RelationTuple) error {
	start := time.Now()
	for _, t := range tuples {
		s.filter.addTuple(t)
	}
	err := s.batch(ctx, tuples, `
		INSERT INTO relation_tuples (object_ns, object_id, relation, subject_ns, subject_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return s.fail(ctx, "tag_w4zvmm", "WriteTuples", err)
	}
	s.log.Debug(ctx, "tag_odkg9p", "tuples written", "count", len(tuples), "took", time.Since(start))
	return nil
}

func (s *PostgresStore) DeleteTuples(ctx context.Context, tuples []model.RelationTuple) error {
	start := time.Now()
	err := s.batch(ctx, tuples, `
		DELETE FROM relation_tuples
		WHERE object_ns = $1 AND object_id = $2 AND relation = $3 AND subject_ns = $4 AND subject_id = $5`)
	if err != nil {
		return s.fail(ctx, "tag_5ktdp0", "DeleteTuples", err)
	}
	s.log.Debug(ctx, "tag_hpqzc0", "tuples deleted", "count", len(tuples), "took", time.Since(start))
	return nil
}

func (s *PostgresStore) batch(ctx context.Context, tuples []model.RelationTuple, query string) error {
	if len(tuples) == 0 {
		return nil
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		for _, t := range tuples {
			b.Queue(query, t.Object.Namespace, t.Object.ID, t.Relation, t.Subject.Namespace, t.Subject.ID)
		}
		return tx.SendBatch(ctx, b).Close()
	})
}

func (s *PostgresStore) ReadTuples(ctx context.Context, obj model.Entity, relation string) ([]model.RelationTuple, error) {
	if !s.filter.mayHaveObject(obj, relation) {
		s.log.Debug(ctx, "tag_kwp6ao", "bloom miss", "op", "ReadTuples", "object", obj, "relation", relation)
		return nil, nil
	}
	start := time.Now()
	rows, err := s.pool.Query(ctx, `
		SELECT subject_ns, subject_id FROM relation_tuples
		WHERE object_ns = $1 AND object_id = $2 AND relation = $3`,
		obj.Namespace, obj.ID, relation)
	if err != nil {
		return nil, s.fail(ctx, "tag_mhzsbj", "ReadTuples", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.RelationTuple, error) {
		t := model.RelationTuple{Object: obj, Relation: relation}
		return t, row.Scan(&t.Subject.Namespace, &t.Subject.ID)
	})
	if err != nil {
		return nil, s.fail(ctx, "tag_2ry90h", "ReadTuples", err)
	}
	s.log.Debug(ctx, "tag_fwrp91", "tuples read", "object", obj, "relation", relation, "count", len(out), "took", time.Since(start))
	return out, nil
}

func (s *PostgresStore) ReadTuplesBySubject(ctx context.Context, sub model.Entity, relation string) ([]model.RelationTuple, error) {
	if !s.filter.mayHaveSubject(sub, relation) {
		s.log.Debug(ctx, "tag_uy5f0w", "bloom miss", "op", "ReadTuplesBySubject", "subject", sub, "relation", relation)
		return nil, nil
	}
	start := time.Now()
	rows, err := s.pool.Query(ctx, `
		SELECT object_ns, object_id FROM relation_tuples
		WHERE subject_ns = $1 AND subject_id = $2 AND relation = $3`,
		sub.Namespace, sub.ID, relation)
	if err != nil {
		return nil, s.fail(ctx, "tag_14t43o", "ReadTuplesBySubject", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.RelationTuple, error) {
		t := model.RelationTuple{Relation: relation, Subject: sub}
		return t, row.Scan(&t.Object.Namespace, &t.Object.ID)
	})
	if err != nil {
		return nil, s.fail(ctx, "tag_n8vklh", "ReadTuplesBySubject", err)
	}
	s.log.Debug(ctx, "tag_v72ama", "tuples read by subject", "subject", sub, "relation", relation, "count", len(out), "took", time.Since(start))
	return out, nil
}

func (s *PostgresStore) ForEachTuple(ctx context.Context, fn func(model.RelationTuple) error) error {
	rows, err := s.pool.Query(ctx, `
		SELECT object_ns, object_id, relation, subject_ns, subject_id FROM relation_tuples`)
	if err != nil {
		return s.fail(ctx, "tag_phcpwk", "ForEachTuple", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t model.RelationTuple
		if err := rows.Scan(&t.Object.Namespace, &t.Object.ID, &t.Relation, &t.Subject.Namespace, &t.Subject.ID); err != nil {
			return s.fail(ctx, "tag_te63ro", "ForEachTuple", err)
		}
		if err := fn(t); err != nil {
			return err
		}
	}
	return s.fail(ctx, "tag_1u0ows", "ForEachTuple", rows.Err())
}

func decodeNamespace(name string, rels []byte) (model.Namespace, error) {
	ns := model.Namespace{Name: name}
	if err := json.Unmarshal(rels, &ns.Relations); err != nil {
		return model.Namespace{}, fmt.Errorf("decode namespace %q: %w", name, err)
	}
	return ns, nil
}
