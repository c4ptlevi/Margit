package bloom

import (
	"strconv"
	"sync"
	"testing"
)

func TestNoFalseNegatives(t *testing.T) {
	f := New(10_000, 0.01)
	for i := range 10_000 {
		f.Add(strconv.Itoa(i))
	}
	for i := range 10_000 {
		if !f.Test(strconv.Itoa(i)) {
			t.Fatalf("false negative for %d", i)
		}
	}
}

func TestFalsePositiveRate(t *testing.T) {
	const n, p = 10_000, 0.01
	f := New(n, p)
	for i := range n {
		f.Add("in-" + strconv.Itoa(i))
	}
	fp := 0
	const trials = 100_000
	for i := range trials {
		if f.Test("out-" + strconv.Itoa(i)) {
			fp++
		}
	}
	if rate := float64(fp) / trials; rate > 2*p {
		t.Fatalf("false positive rate %.4f, want <= %.4f", rate, 2*p)
	}
}

func TestEmpty(t *testing.T) {
	f := New(0, 0.01)
	if f.Test("anything") {
		t.Fatal("empty filter reported a hit")
	}
}

func TestConcurrent(t *testing.T) {
	f := New(1_000, 0.01)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 100 {
				key := strconv.Itoa(g*1000 + i)
				f.Add(key)
				if !f.Test(key) {
					t.Errorf("false negative for %s", key)
				}
			}
		}()
	}
	wg.Wait()
}
