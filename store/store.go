package store

import (
	"context"

	"github.com/c4ptlevi/margit/model"
)

type Store interface {
	SaveNamespace(ctx context.Context, ns model.Namespace) error
	GetNamespace(ctx context.Context, name string) (model.Namespace, error)
	ListNamespaces(ctx context.Context) ([]model.Namespace, error)
	DeleteNamespace(ctx context.Context, name string) error
	NamespaceExists(ctx context.Context, name string) (bool, error)
	RelationExists(ctx context.Context, namespace string, relation string) (bool, error)

	WriteTuples(ctx context.Context, tuples []model.RelationTuple) error
	DeleteTuples(ctx context.Context, tuples []model.RelationTuple) error

	ReadTuples(ctx context.Context, obj model.Entity, relation string) ([]model.RelationTuple, error)
	ReadTuplesBySubject(ctx context.Context, sub model.Entity, relation string) ([]model.RelationTuple, error)
	ForEachTuple(ctx context.Context, fn func(model.RelationTuple) error) error
}
