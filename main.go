package main

import (
	"log"
	"net/http"

	"wallet-transfer-assignment/db"
	"wallet-transfer-assignment/handler"
)

func main() {
	db.InitDB()
	db.InitSchema()

	http.HandleFunc("/transfers", handler.TransferHandler)

	log.Println("Server running on :8080")
	http.ListenAndServe(":8080", nil)
}
