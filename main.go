package main

//go:generate go run ./cmd/tagger

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/c4ptlevi/margit/api"
	"github.com/c4ptlevi/margit/config"
	"github.com/c4ptlevi/margit/engine"
	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/store"
)

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = logger.StartTrace(ctx)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.New(os.Stderr, logger.LevelError).Error(ctx, "tag_bcd8de", "config load failed", "path", *configPath, "err", err)
		return 1
	}
	log := logger.New(os.Stdout, cfg.LogLevel)
	log.Info(ctx, "tag_fqgc9d", "margit starting", "config", *configPath, "log_level", cfg.LogLevel)
	log.Info(ctx, "tag_lcxi73", "config loaded", "store", cfg.Store.Type, "bloom_expected", cfg.Store.Postgres.BloomExpected,
		"bloom_fp_rate", cfg.Store.Postgres.BloomFPRate, "max_depth", cfg.Engine.MaxDepth,
		"lookup_default_limit", cfg.Engine.LookupLimit, "lookup_max_limit", cfg.Engine.MaxLookupLimit,
		"addr", cfg.Server.Addr, "read_timeout", cfg.Server.ReadTimeout, "write_timeout", cfg.Server.WriteTimeout)

	st, closeStore, err := store.Open(ctx, cfg.Store, log)
	if err != nil {
		log.Error(ctx, "tag_jmetbn", "store open failed", "type", cfg.Store.Type, "err", err)
		return 1
	}
	defer closeStore()

	var eng engine.ReBACEngine = engine.New(st, &engine.MemExprCache{Log: log}, cfg.Engine, log)
	if cfg.Engine.Cache.TTLMillis > 0 {
		eng = engine.NewCached(eng, engine.NewMemResponseCache(ctx, cfg.Engine.Cache, log), log)
	}
	log.Info(ctx, "tag_mx31g8", "engine ready", "max_depth", cfg.Engine.MaxDepth,
		"cache_ttl_ms", cfg.Engine.Cache.TTLMillis, "cache_max_entries", cfg.Engine.Cache.MaxEntries)

	if err := api.New(eng, log).Run(ctx, cfg.Server); err != nil {
		log.Error(ctx, "tag_x5ba4a", "margit exiting", "err", err)
		return 1
	}
	log.Info(ctx, "tag_a8l7og", "margit stopped")
	return 0
}
