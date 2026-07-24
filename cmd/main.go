package main

import (
	"log"
	"net/http"
	"wallet-assignment/internal/db"
	"wallet-assignment/internal/domain"
	"wallet-assignment/internal/handler"
	"wallet-assignment/internal/service"
)

func main() {
	database := db.Init()

	database.FirstOrCreate(&domain.Wallet{ID: "wallet_1"}, domain.Wallet{Balance: 1000})
	database.FirstOrCreate(&domain.Wallet{ID: "wallet_2"}, domain.Wallet{Balance: 1000})

	svc := service.NewTransferService(database)
	h := handler.NewHandler(svc)

	http.HandleFunc("/transfers", h.Transfer)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		if _, err := w.Write([]byte("OK")); err != nil {
			log.Println("write error:", err)
		}
	})


	log.Println("Starting HTTP server on :8080")


	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
