package main

import (
	"github.com/gerry1818/wallet-transfer-assignment/internal/logger"
	"github.com/gerry1818/wallet-transfer-assignment/internal/metrics"
	"github.com/gerry1818/wallet-transfer-assignment/internal/server"
)

func main() {
	// Init logger
	logger.Init()
	defer logger.Log.Sync()

	logger.Log.Info("starting wallet transfer service")

	// Init metrics
	metrics.Init()

	// Create server with default config
	cfg := server.DefaultConfig()
	srv, err := server.New(cfg)
	if err != nil {
		panic(err)
	}

	// Start server
	if err := srv.Start(); err != nil {
		panic(err)
	}
}
