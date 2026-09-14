// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"agrelha/internal/platform/build"
	"agrelha/internal/platform/config"
	"agrelha/internal/platform/logging"
	"agrelha/internal/wiring"
)

func main() {
	cfg := config.Load()
	logging.Setup(cfg.LogLevel, cfg.LogFormat)
	build.SourceURL = cfg.SourceURL

	deps, err := wiring.Build(context.Background(), cfg)
	if err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}

	app := wiring.BuildServer(context.Background(), cfg, deps)

	go func() {
		if err := app.Listen(cfg.ListenAddr); err != nil {
			slog.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()
	slog.Info("agrelha listening", "version", build.Version, "commit", build.Commit, "built", build.Date,
		"addr", cfg.ListenAddr, "log_level", cfg.LogLevel, "log_format", cfg.LogFormat)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	slog.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.ShutdownWithContext(shutCtx); err != nil {
		slog.Error("forced shutdown", "err", err)
		os.Exit(1)
	}
}
