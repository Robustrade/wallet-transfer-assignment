package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"wallet-transfer-assignment/internal/app"
	"wallet-transfer-assignment/internal/platform/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	a, err := app.New(ctx, cfg)
	if err != nil {
		log.Fatalf("init app: %v", err)
	}
	log.Printf("listening on :%d", cfg.Port)
	if err := a.Run(ctx); err != nil {
		log.Fatalf("run: %v", err)
	}
}
