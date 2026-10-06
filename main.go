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

	st, closeStore, err := store.Open(ctx, cfg.Store, log)
	if err != nil {
		log.Error(ctx, "tag_jmetbn", "store open failed", "type", cfg.Store.Type, "err", err)
		return 1
	}
	defer closeStore()

	eng := engine.New(st, &engine.MemExprCache{}, cfg.Engine, log)
	log.Info(ctx, "tag_mx31g8", "engine ready", "max_depth", cfg.Engine.MaxDepth)

	if err := api.New(eng, log).Run(ctx, cfg.Server); err != nil {
		return 1
	}
	log.Info(ctx, "tag_a8l7og", "margit stopped")
	return 0
}
