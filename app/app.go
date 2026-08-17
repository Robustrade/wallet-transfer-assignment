package app

import (
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/raknay/wallet-transfer-assignment/db"
	handler "github.com/raknay/wallet-transfer-assignment/internal/wallet/handler/http"
	"github.com/raknay/wallet-transfer-assignment/internal/wallet/repository"
	"github.com/raknay/wallet-transfer-assignment/internal/wallet/service"
	"github.com/raknay/wallet-transfer-assignment/pkg/config"
)

func initRoutes() *chi.Mux {
	// initialize db
	conf, err := config.LoadPostgresConfig()
	if err != nil {
		log.Fatalf("Error during DB config: %v", err)
	}

	pool, err := db.Connection(conf.DbUrl)
	if err != nil {
		log.Fatalf("Error during DB config: %v", err)
	}
	log.Println("Connection to DB was successful")

	router := chi.NewRouter()

	// wallet domain
	walletRepo := repository.NewWalletRepo(pool)
	walletService := service.NewWalletService(walletRepo)
	handler.NewWalletHandler(router, walletService)

	return router
}

func Run() {
	router := initRoutes()
	server := &http.Server{
		Addr:    "127.0.0.1:8000",
		Handler: router,
	}
	log.Println("server running on :8000")
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
