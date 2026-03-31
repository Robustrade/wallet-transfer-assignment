package main

import (
	"database/sql"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	_ "github.com/lib/pq"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL must be set")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping: %v", err)
	}

	path := filepath.Join("db", "migrations", "000001_init_schema.up.sql")
	content, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read schema: %v", err)
	}

	_, err = db.Exec(string(content))
	if err != nil {
		log.Fatalf("Failed to execute schema: %v", err)
	}

	log.Println("Successfully applied database migrations!")
}
