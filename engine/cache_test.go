package engine

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/c4ptlevi/margit/model"
)

func TestExprCacheGet(t *testing.T) {
	var c ExprCache = &MemExprCache{}
	r := model.Relation{Name: "can_view", RelExpr: "owner + viewer"}
	first, err := c.Get(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Get(context.Background(), model.Relation{Name: "other", RelExpr: "owner + viewer"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.String() != "owner + viewer" {
		t.Fatalf("got %v and %v", first, second)
	}
}

func TestExprCacheInvalid(t *testing.T) {
	var c ExprCache = &MemExprCache{}
	if _, err := c.Get(context.Background(), model.Relation{Name: "bad", RelExpr: "owner +"}); !errors.Is(err, model.ErrInvalidExpression) {
		t.Fatalf("err = %v, want ErrInvalidExpression", err)
	}
}

func TestExprCacheConcurrent(t *testing.T) {
	var c ExprCache = &MemExprCache{}
	r := model.Relation{Name: "can_view", RelExpr: "a + b - c"}
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Get(context.Background(), r); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
