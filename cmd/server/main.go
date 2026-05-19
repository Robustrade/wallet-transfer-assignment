package main

import (
	"context"
	"log"
	"wallet-transfer-assignment/cmd"
	"wallet-transfer-assignment/internal/api/routes"
	"wallet-transfer-assignment/internal/wallet"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()

	config, err := cmd.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to initialise config %v", err)
	}

	ctx := context.Background()
	deps, err := cmd.InitDependencies(ctx, config)
	if err != nil {
		log.Fatalf("DB connection error %v", err)
	}

	walletRepository := wallet.NewRepository(deps.Database)
	walletService := wallet.NewService(walletRepository)

	routes.RegisterHealthRoute(app)
	routes.RegisterWalletRoutes(app, walletService)

	if err := app.Listen(":8080"); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
