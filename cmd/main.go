package main

import (
	"context"
	"log"
	"os"
	"wallet-transfer-service/internal/handler"
	"wallet-transfer-service/internal/repository"
	"wallet-transfer-service/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}

	dbURL := os.Getenv("DATABASE_URL")

	conn, err := pgx.Connect(context.Background(), dbURL)
	if err != nil {
		log.Fatal("failed to connect database:", err)
	}

	defer conn.Close(context.Background())

	walletRepo := repository.NewWalletRepository(conn)

	walletService := service.NewWalletService(walletRepo)

	walletHandler := handler.NewWalletHandler(walletService)

	router := gin.Default()

	router.GET("/wallets/:id", walletHandler.GetWallet)

	err = router.Run(":8080")
	if err != nil {
		log.Fatal(err)
	}
}
