package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sonalijais/wallet-transfer-assignment/internal/db"
	"github.com/sonalijais/wallet-transfer-assignment/internal/handler"
	"github.com/sonalijais/wallet-transfer-assignment/internal/repository"
	"github.com/sonalijais/wallet-transfer-assignment/internal/service"
)

func main() {
	dbConn, err := db.NewPostgres("postgres://wallet_user:wallet_pass@localhost:5432/wallet?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}

	repo := repository.New(dbConn)
	svc := &service.Service{Repo: repo, DB: dbConn}
	h := &handler.Handler{Service: svc}

	r := chi.NewRouter()
	r.Post("/transfers", h.CreateTransfer)

	log.Println("Server running on :8080")
	http.ListenAndServe(":8080", r)
}
