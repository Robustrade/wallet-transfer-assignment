package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-playground/validator/v10"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/labstack/echo/v4"

	"wallet-transfer-assignment/ent"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository/entrepo"
	"wallet-transfer-assignment/internal/service"
)

type CustomValidator struct {
	validator *validator.Validate
}

func (cv *CustomValidator) Validate(i any) error {
	return cv.validator.Struct(i)
}

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://postgres:postgres@localhost:5432/wallet_transfer_test?sslmode=disable"
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("ping database: %v", err)
	}

	driver := entsql.OpenDB(
		dialect.Postgres,
		db,
	)

	client := ent.NewClient(
		ent.Driver(driver),
	)

	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("close ent client: %v", err)
		}
	}()

	// Fine for this assignment/dev environment.
	// For a larger production system we'd normally use explicit migrations.
	if err := client.Schema.Create(context.Background()); err != nil {
		log.Fatalf("create database schema: %v", err)
	}

	txManager := entrepo.NewTransactionManager(client)
	transferRepo := entrepo.NewTransferRepository(client)

	transferService := service.NewTransferService(
		txManager,
		transferRepo,
	)

	transferHandler := handler.NewTransferHandler(
		transferService,
	)

	e := echo.New()
	e.Validator = &CustomValidator{
		validator: validator.New(),
	}

	e.POST(
		"/transfers",
		transferHandler.CreateTransfer,
	)

	e.GET(
		"/health",
		func(c echo.Context) error {
			return c.JSON(
				http.StatusOK,
				map[string]string{
					"status": "ok",
				},
			)
		},
	)

	go func() {
		address := ":8080"

		log.Printf(
			"wallet transfer service listening on %s",
			address,
		)

		if err := e.Start(address); err != nil &&
			err != http.ErrServerClosed {

			log.Fatalf("start HTTP server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)

	signal.Notify(
		quit,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	<-quit

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		log.Printf(
			"HTTP server shutdown error: %v",
			err,
		)
	}
}
