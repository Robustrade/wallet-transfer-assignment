package main

import (
	"log"
	"net/http"

	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/platform"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"
)

func main() {
	db, err := platform.NewDatabase()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	transferRepo := repository.NewTransferRepository(db)
	transferService := service.NewTransferService(transferRepo)
	transferHandler := handler.NewTransferHandler(transferService)

	mux := http.NewServeMux()
	transferHandler.RegisterRoutes(mux)

	log.Println("wallet transfer service listening on :8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatal(err)
	}
}
