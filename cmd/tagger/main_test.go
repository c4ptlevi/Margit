package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

const sample = `package demo

type logger struct{}

func (logger) Info(ctx any, tag, msg string) {}

func fail(ctx any, tag string, err error) error { return err }

func other(ctx any, name string) {}

func f(log logger, ctx any) {
	fail(ctx, "0000", nil)
	other(ctx, "0000")
	log.Info(ctx, "0000", "first")
	log.Info(ctx, "", "empty")
	log.Info(ctx, "abc123", "second")
	log.Info(ctx, "abc123", "dup")
	log.Info(ctx, "BAD", "bad")
	log.Info(ctx, "tag_xyz789", "already prefixed")
	println("", "not a log call")
}
`

func TestRunFillsTags(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "demo.go")
	if err := os.WriteFile(file, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo_test.go"), []byte(`package demo
func g(log interface{ Info(any, string, string) }) { log.Info(nil, "", "x") }
`), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := run(dir, true, io.Discard)
	if err != nil || n != 6 {
		t.Fatalf("check: n = %d, err = %v, want 6", n, err)
	}

	if n, err := run(dir, false, io.Discard); err != nil || n != 6 {
		t.Fatalf("fix: n = %d, err = %v, want 6", n, err)
	}
	out, _ := os.ReadFile(file)

	tags := regexp.MustCompile(`log\.Info\(ctx, "([^"]*)"`).FindAllStringSubmatch(string(out), -1)
	if len(tags) != 6 {
		t.Fatalf("found %d log calls in:\n%s", len(tags), out)
	}
	valid := regexp.MustCompile(`^tag_[a-z0-9]{6}$`)
	seen := map[string]bool{}
	for _, m := range tags {
		if !valid.MatchString(m[1]) || seen[m[1]] {
			t.Fatalf("bad or duplicate tag %q in:\n%s", m[1], out)
		}
		seen[m[1]] = true
	}
	if tags[2][1] != "tag_abc123" {
		t.Fatalf("existing tag not prefixed in place: %q", tags[2][1])
	}
	if tags[5][1] != "tag_xyz789" {
		t.Fatalf("prefixed tag changed to %q", tags[5][1])
	}
	if !regexp.MustCompile(`fail\(ctx, "tag_[a-z0-9]{6}"`).Match(out) {
		t.Fatalf("helper with tag param not filled:\n%s", out)
	}
	if !regexp.MustCompile(`other\(ctx, "0000"\)`).Match(out) {
		t.Fatalf("function without tag param modified:\n%s", out)
	}
	if !regexp.MustCompile(`println\("", "not a log call"\)`).Match(out) {
		t.Fatalf("non-log call modified:\n%s", out)
	}

	if n, err := run(dir, true, io.Discard); err != nil || n != 0 {
		t.Fatalf("recheck: n = %d, err = %v, want 0", n, err)
	}
}
