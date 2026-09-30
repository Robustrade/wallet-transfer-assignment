package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/robustrade/wallet-transfer/internal/config"
	"github.com/robustrade/wallet-transfer/internal/httpapi"
	"github.com/robustrade/wallet-transfer/internal/service"
	"github.com/robustrade/wallet-transfer/internal/store"
)

func main() {
	if envFile := os.Getenv("ENV_FILE"); envFile != "" {
		if err := config.LoadEnvFile(envFile); err != nil {
			log.Fatalf("load %s: %v", envFile, err)
		}
	}
	databaseURL := flag.String("db-url", firstNonEmpty(os.Getenv("DATABASE_URL"), os.Getenv("DB_URL")), "PostgreSQL URL (overrides individual DB settings)")
	databaseHost := flag.String("db-host", os.Getenv("DB_HOST"), "PostgreSQL host")
	databasePort := flag.String("db-port", os.Getenv("DB_PORT"), "PostgreSQL port")
	databaseUser := flag.String("db-user", os.Getenv("DB_USER"), "PostgreSQL user")
	databasePassword := flag.String("db-password", os.Getenv("DB_PASSWORD"), "PostgreSQL password")
	databaseName := flag.String("db-name", os.Getenv("DB_NAME"), "PostgreSQL database name")
	databaseSSLMode := flag.String("db-sslmode", os.Getenv("DB_SSL_MODE"), "PostgreSQL SSL mode (defaults to disable)")
	port := flag.String("port", envOr("PORT", "8080"), "HTTP listen port")
	flag.Parse()
	resolvedDatabaseURL, err := config.DatabaseURL(*databaseURL, *databaseHost, *databasePort, *databaseUser, *databasePassword, *databaseName, *databaseSSLMode)
	if err != nil {
		log.Fatal(err)
	}
	if resolvedDatabaseURL == "" {
		log.Fatal("configure -db-url or DB_HOST, DB_USER, and DB_NAME")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, resolvedDatabaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping database: %v", err)
	}
	repository := store.NewPostgres(pool)
	if err := repository.Migrate(ctx); err != nil {
		log.Fatalf("apply schema: %v", err)
	}

	e := echo.New()
	e.HideBanner = false
	e.Use(middleware.Recover(), middleware.RequestID(), middleware.Logger())
	httpapi.NewHandler(service.NewTransfers(repository)).Register(e)
	log.Fatal(e.Start(":" + *port))
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
