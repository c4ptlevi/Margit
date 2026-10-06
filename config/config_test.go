package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/store"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	t.Setenv("TEST_PG_DSN", "postgres://x")
	cfg, err := Load(writeConfig(t, `{
		"log_level": "debug",
		"store": {"type": "postgres", "postgres": {"dsn": "${TEST_PG_DSN}", "bloom_expected": 500}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != logger.LevelDebug || cfg.Store.Type != store.KindPostgres {
		t.Fatalf("cfg = %+v", cfg)
	}
	pg := cfg.Store.Postgres
	if pg.DSN != "postgres://x" || pg.BloomExpected != 500 || pg.BloomFPRate != 0.01 {
		t.Fatalf("postgres cfg = %+v", pg)
	}
}

func TestLoadDefaultsForMissingKeys(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Default() {
		t.Fatalf("cfg = %+v, want defaults", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file err = %v", err)
	}
	if _, err := Load(writeConfig(t, `{"store": {"typo": "memory"}}`)); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := Load(writeConfig(t, `{"log_level": "verbose"}`)); err == nil {
		t.Fatal("bad log level accepted")
	}
}
