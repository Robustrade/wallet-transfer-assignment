package database

import (
	"log"
	"time"

	"wallet-transfer/db/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func Connection() (*gorm.DB, error) {
	var err error

	dsn := config.Configuration()

	const maxRetries = 10
	const retryDelay = 1 * time.Second
	for i := 0; i < maxRetries; i++ {
		// db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			log.Printf("database connection failed due to connection creationg using gorm.Open() %v times and error is %v\n", i, err)
			time.Sleep(retryDelay)
			continue
		}

		// Get generic database object sql.DB to use its functions
		sqlDB, err1 := db.DB()

		if err1 != nil {
			log.Printf("Failed to get underlying sql.DB: %v. Retrying in %v...", err1, retryDelay)
			time.Sleep(retryDelay)
			continue
		}
		sqlDB.SetMaxOpenConns(80)
		sqlDB.SetMaxIdleConns(80)
		sqlDB.SetConnMaxLifetime(10 * time.Minute)
		sqlDB.SetConnMaxIdleTime(5 * time.Minute)

		if err2 := sqlDB.Ping(); err2 != nil {
			sqlDB.Close()
			log.Printf("database connection failed %v times after checking ping database and error is %v\n", i, err)
			continue
		} else {
			log.Println("successfully database connection pool...")
			return db, nil
		}
	}
	return nil, err
}
