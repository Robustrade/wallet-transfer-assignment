package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"wallet-transfer-assignment/internal/transfer"

	_ "modernc.org/sqlite"
)

func main() {
	databasePath := os.Getenv("DATABASE_PATH")
	if databasePath == "" {
		databasePath = "wallets.db"
	}

	db, err := sql.Open("sqlite", "file:"+databasePath+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store := transfer.NewStore(db)
	if err := store.Initialize(); err != nil {
		log.Fatal(err)
	}

	server := transfer.NewHTTPServer(transfer.NewService(store))
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = ":8080"
	}

	log.Printf("wallet transfer service listening on %s", address)
	srv := &http.Server{
		Addr:              address,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * 1e9,
		IdleTimeout:       30 * 1e9,
	}
	log.Fatal(srv.ListenAndServe())
}
