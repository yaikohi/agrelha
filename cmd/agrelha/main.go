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
	"agrelha/internal/server"
)

func main() {
	cfg := config.Load()
	logging.Setup(cfg.LogLevel, cfg.LogFormat)
	build.SourceURL = cfg.SourceURL

	srv := server.New(cfg)
	srv.RegisterFiberRoutes()

	go func() {
		if err := srv.Listen(cfg.ListenAddr); err != nil {
			slog.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()
	slog.Info("agrelha listening", "version", build.Version, "addr", cfg.ListenAddr, "log_level", cfg.LogLevel, "log_format", cfg.LogFormat)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	slog.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.ShutdownWithContext(shutCtx); err != nil {
		slog.Error("forced shutdown", "err", err)
		os.Exit(1)
	}
}
