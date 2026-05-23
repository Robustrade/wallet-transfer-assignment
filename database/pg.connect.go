package database

import (
	"database/sql"
	"fmt"
	"log"
	"shindechandrakant/shared/env"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func Init() {
	host := env.GetString("POSTGRES_HOST", "")
	port := env.GetString("POSTGRES_PORT", "")
	user := env.GetString("POSTGRES_USER", "")
	password := env.GetString("POSTGRES_PASSWORD", "")
	dbname := env.GetString("POSTGRES_DB", "")

	if host == "" || port == "" || user == "" || password == "" || dbname == "" {
		log.Fatal("Database credentials are not fully set in the environment variables")
	}

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	var err error

	DB, err = sql.Open("postgres", dsn)

	if err != nil {
		log.Fatalf("Failed to open db connection: %v", err)
	}

	if err := DB.Ping(); err != nil {
		log.Fatalf("Failed to ping DB: %v", err)
	}

	fmt.Println("PG Database connected successfully...")
}

func Close() {
	if DB == nil {
		return
	}
	//
	//ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	//defer cancel()
	if err := DB.Close(); err != nil {

		log.Printf("Unable to disconnect db. Error: %+v", err)
	}
	log.Println("Postgres Disconnected")
}
