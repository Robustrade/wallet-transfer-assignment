package main

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Robustrade/wallet-transfer-assignment/wallet-service/internal/handler"
)

func main() {
	r := chi.NewRouter()
	r.Get("/health", handler.HealthHandler)

	fmt.Println("Server started on :8080")

	http.ListenAndServe(":8080", r)
}
