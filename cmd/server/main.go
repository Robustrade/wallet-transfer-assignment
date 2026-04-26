package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/robustrade/wallet-transfer-assignment/config"
	"github.com/robustrade/wallet-transfer-assignment/internal/balance"
	"github.com/robustrade/wallet-transfer-assignment/internal/db"
	"github.com/robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/robustrade/wallet-transfer-assignment/internal/kafka"
	"github.com/robustrade/wallet-transfer-assignment/internal/ledger"
	"github.com/robustrade/wallet-transfer-assignment/internal/outbox"
	"github.com/robustrade/wallet-transfer-assignment/internal/service"
	transferRepository "github.com/robustrade/wallet-transfer-assignment/internal/transfer"
)

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {

		configPath = "config/config.yaml"

	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)

	}

	database, err := db.NewPostgresDB(cfg.Database)
	if err != nil {
		log.Fatalf("failed to connect DB: %v", err)
	}
	defer database.Close()

	transferRepo := transferRepository.NewTransferRepository(database)
	ledgerRepo := ledger.NewLedgerRepository(database)
	balanceRepo := balance.NewBalanceRepository(database)
	transferService := service.NewTransferService(database, transferRepo)
	transferHandler := handler.NewTransferHandler(transferService)

	// Create Kafka publisher and consumer
	kafkaPublisher := kafka.NewKafkaPublisher()
	outboxRepo := outbox.NewPostgresRepository(database)
	outboxWorker := outbox.NewWorker(outboxRepo, kafkaPublisher)
	ledger_consumer := kafka.NewLedgerConsumer(database, ledgerRepo, kafkaPublisher)
	balance_consumer := kafka.NewBalanceConsumer(database, balanceRepo)
	defer ledger_consumer.Close()
	defer balance_consumer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go outboxWorker.Start(ctx)
	go ledger_consumer.StartLedgerConsumer(ctx)
	go balance_consumer.StartBalanceConsumer(ctx)

	r := gin.Default()

	r.POST("/transfer", transferHandler.Transfer)

	// Setup graceful shutdown
	go func() {
		log.Printf("server running on port %s", cfg.Server.Port)
		if err := r.Run(":" + cfg.Server.Port); err != nil {
			log.Fatalf("failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Cancel context to stop consumer
	cancel()

	// Give consumer time to finish processing
	time.Sleep(2 * time.Second)
	log.Println("Server exited")
}
