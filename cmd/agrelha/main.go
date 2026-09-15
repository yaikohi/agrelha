// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"io"
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

var (
	osExit          = os.Exit
	shutdownTimeout = 5 * time.Second
)

func main() {
	osExit(run(context.Background(), os.Stdout, os.Stderr, os.Args[1:]))
}

func run(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	cfg := config.Load()
	logging.Setup(cfg.LogLevel, cfg.LogFormat)
	build.SourceURL = cfg.SourceURL

	deps, err := wiring.Build(ctx, cfg)
	if err != nil {
		slog.Error("startup failed", "err", err)
		return 1
	}

	app := wiring.BuildServer(ctx, cfg, deps)

	errCh := make(chan error, 1)
	go func() {
		if err := app.Listen(cfg.ListenAddr); err != nil {
			errCh <- err
		}
	}()
	slog.Info("agrelha listening", "version", build.Version, "commit", build.Commit, "built", build.Date,
		"addr", cfg.ListenAddr, "log_level", cfg.LogLevel, "log_format", cfg.LogFormat)

	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		slog.Error("http server error", "err", err)
		return 1
	case <-sigCtx.Done():
		slog.Info("shutting down")
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := app.ShutdownWithContext(shutCtx); err != nil {
		slog.Error("forced shutdown", "err", err)
		return 1
	}
	return 0
}
