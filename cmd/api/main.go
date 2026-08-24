package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"agrelha/internal/config"
	"agrelha/internal/server"
)

func main() {
	cfg := config.Load()
	srv := server.New(cfg)
	srv.RegisterFiberRoutes()

	go func() {
		if err := srv.Listen(cfg.ListenAddr); err != nil {
			log.Fatalf("http server error: %v", err)
		}
	}()
	log.Printf("agrelha listening on %s", cfg.ListenAddr)

	// Graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Println("shutting down…")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.ShutdownWithContext(shutCtx); err != nil {
		log.Printf("forced shutdown: %v", err)
		os.Exit(1)
	}
}
