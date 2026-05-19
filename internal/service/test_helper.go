package service

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

func setupTestDB(t *testing.T) *pgx.Conn {

	err := godotenv.Load("../../.env")
	if err != nil {
		t.Fatal(err)
	}

	dbURL := os.Getenv("DATABASE_URL")

	conn, err := pgx.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}

	return conn
}
