package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"shindechandrakant/database"
	"shindechandrakant/internal/api/controllers"
	"shindechandrakant/internal/api/routes"
	"shindechandrakant/internal/wallet"
	"syscall"
	"time"

	"shindechandrakant/shared/env"

	"github.com/gofiber/fiber/v3"
)

func main() {
	env.Init(".env")
	database.Init()
	database.Migrate()
	database.Seed()

	ServerPort := env.GetString("SERVER_PORT", "8080")
	app := fiber.New()
	api := app.Group("/api")
	walletRepository := wallet.NewWalletRepository(database.DB)
	walletService := wallet.NewWalletService(walletRepository)
	walletController := controllers.NewWalletController(walletService)
	routes.WalletRoutes(api, walletController)

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
		<-quit
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.ShutdownWithContext(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		} else {
			log.Println("Server exited properly")
		}
		database.Close()
	}()
	if err := app.Listen(fmt.Sprintf(":%s", ServerPort), fiber.ListenConfig{EnablePrintRoutes: true}); err != nil {
		log.Fatalf("Server failed to start at %s, error: %+v", ServerPort, err)
	}

	fmt.Println("Hello Wallet balance...")
}
