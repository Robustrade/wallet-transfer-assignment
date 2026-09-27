package main

import (
	"log"
	"net/http"

	"wallet-transfer-assignment/internal/database"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/service"
)

func main() {
	db, err := database.Connect()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	repo := repository.New(db)
	transferService := service.NewTransferService(db, repo)
	transferHandler := handler.NewTransferHandler(transferService)

	mux := http.NewServeMux()
	mux.HandleFunc("/transfers", transferHandler.CreateTransfer)

	log.Println("server running on :8080")

	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
