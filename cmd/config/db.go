package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func ConnectDB() *gorm.DB {
	// Load .env file
	// We ignore the error because environment variables might be provided directly (e.g., in Docker)
	_ = godotenv.Load()

	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	port := os.Getenv("DB_PORT")

	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		host,
		user,
		password,
		dbName,
		port,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect database: ", err)
	}
	log.Println("Database connected successfully")

	return db
}

func RunMigrations(db *gorm.DB) {
	// Run migrations
	err := db.AutoMigrate(
		&models.Wallet{},
		&models.Transfer{},
		&models.LedgerEntry{},
		&models.IdempotencyRecord{},
	)
	if err != nil {
		log.Fatal("Failed to run migrations: ", err)
	}
	log.Println("Migrations completed successfully")
}
