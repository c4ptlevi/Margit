package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

type Duration time.Duration

func (d *Duration) UnmarshalText(b []byte) error {
	parsed, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) String() string {
	return time.Duration(d).String()
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

type Config struct {
	Addr            string   `json:"addr"`
	ReadTimeout     Duration `json:"read_timeout"`
	WriteTimeout    Duration `json:"write_timeout"`
	ShutdownTimeout Duration `json:"shutdown_timeout"`
}

func DefaultConfig() Config {
	return Config{
		Addr:            ":8080",
		ReadTimeout:     Duration(10 * time.Second),
		WriteTimeout:    Duration(30 * time.Second),
		ShutdownTimeout: Duration(15 * time.Second),
	}
}

func (s *Server) Run(ctx context.Context, cfg Config) error {
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		s.log.Error(ctx, "tag_w747dx", "listen failed", "addr", cfg.Addr, "err", err)
		return err
	}
	return s.Serve(ctx, ln, cfg)
}

func (s *Server) Serve(ctx context.Context, ln net.Listener, cfg Config) error {
	srv := &http.Server{
		Handler:      s,
		ReadTimeout:  time.Duration(cfg.ReadTimeout),
		WriteTimeout: time.Duration(cfg.WriteTimeout),
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	s.log.Info(ctx, "tag_g5qhck", "http server listening", "addr", ln.Addr(),
		"read_timeout", cfg.ReadTimeout, "write_timeout", cfg.WriteTimeout, "shutdown_timeout", cfg.ShutdownTimeout)

	select {
	case err := <-errCh:
		s.log.Error(ctx, "tag_6i70zw", "http server stopped", "err", err)
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(cfg.ShutdownTimeout))
	defer cancel()
	start := time.Now()
	s.log.Info(ctx, "tag_xwm3vo", "http server shutting down", "reason", context.Cause(ctx))
	if err := srv.Shutdown(shutdownCtx); err != nil {
		s.log.Error(ctx, "tag_g8dc13", "http server shutdown failed", "err", err, "took", time.Since(start))
		return err
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		s.log.Error(ctx, "tag_r6u4fj", "http server exited unexpectedly", "err", err)
		return err
	}
	s.log.Info(ctx, "tag_7gd3lb", "http server stopped", "took", time.Since(start))
	return nil
}
