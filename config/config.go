package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/c4ptlevi/margit/api"
	"github.com/c4ptlevi/margit/engine"
	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/store"
)

type Config struct {
	LogLevel logger.Level  `json:"log_level"`
	Store    store.Config  `json:"store"`
	Engine   engine.Config `json:"engine"`
	Server   api.Config    `json:"server"`
}

func Default() Config {
	return Config{
		LogLevel: logger.LevelInfo,
		Store: store.Config{
			Type: store.KindMemory,
			Postgres: store.PostgresConfig{
				BloomExpected: 1_000_000,
				BloomFPRate:   0.01,
			},
		},
		Engine: engine.Config{
			MaxDepth:       engine.DefaultMaxDepth,
			LookupLimit:    engine.DefaultLookupLimit,
			MaxLookupLimit: engine.DefaultMaxLookupLimit,
		},
		Server: api.DefaultConfig(),
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(os.ExpandEnv(string(raw)))))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}
