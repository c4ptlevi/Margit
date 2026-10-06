package store

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
)

func TestMemoryStore(t *testing.T) {
	runStoreTests(t, func(*testing.T) Store { return NewMemoryStore(nil) })
}

func TestMemoryStoreLogsWithTrace(t *testing.T) {
	var buf bytes.Buffer
	s := NewMemoryStore(logger.New(&buf, logger.LevelDebug))
	ctx := logger.WithTraceID(context.Background(), "req42")
	doc := model.Entity{Namespace: "document", ID: "1"}
	alice := model.Entity{Namespace: "user", ID: "alice"}
	if err := s.WriteTuples(ctx, []model.RelationTuple{{Object: doc, Relation: "viewer", Subject: alice}}); err != nil {
		t.Fatal(err)
	}
	line := buf.String()
	if !strings.Contains(line, " DEBUG ") || !strings.Contains(line, "trace=req42") || !strings.Contains(line, "count=1") {
		t.Fatalf("log line = %q", line)
	}
}
