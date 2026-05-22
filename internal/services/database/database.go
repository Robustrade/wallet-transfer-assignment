package database

import (
	"fmt"
	"log"

	"wallet-transfer-assignment/internal/models"
	"wallet-transfer-assignment/internal/services/environment"
	"wallet-transfer-assignment/internal/utils"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDB initializes the PostgreSQL connection using GORM and sets session-level timeouts.
func InitDB() {
	// Construct the DSN string with explicit lock and statement timeouts for safety.
	dsnFmt := "host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC " +
		"options='-c lock_timeout=%sms -c statement_timeout=%sms'"
	dsn := fmt.Sprintf(dsnFmt,
		environment.GoDotEnvVariable("DB_HOST", "localhost"),
		environment.GoDotEnvVariable("DB_USER", "postgres"),
		environment.GoDotEnvVariable("DB_PASSWORD", "postgres"),
		environment.GoDotEnvVariable("DB_NAME", "wallet_transfer"),
		environment.GoDotEnvVariable("DB_PORT", "5432"),
		environment.GoDotEnvVariable("DB_LOCK_TIMEOUTS", "32000"),
		environment.GoDotEnvVariable("DB_STATEMENT_TIMEOUTS", "30000"),
	)

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	}

	// Adjust logging level based on the DEBUG environment variable.
	if !utils.GetBoolFromString(environment.GoDotEnvVariable("DEBUG", "true")) {
		gormConfig = &gorm.Config{
			Logger: logger.Default.LogMode(logger.Error),
		}
	}

	// Connect to the database.
	db, err := gorm.Open(postgres.Open(dsn), gormConfig)
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	// Initialize schema and triggers.
	InitializeSchema(db)

	DB = db
}

// InitializeSchema handles table migrations and trigger enforcement.
// This is exported so integration tests can ensure the same schema is applied to test databases.
func InitializeSchema(db *gorm.DB) {
	err := db.AutoMigrate(
		&models.Wallet{},
		&models.Transfer{},
		&models.Ledger{})
	if err != nil {
		log.Printf("initial migration failed: %v", err)
	}

	// Harden the database by enforcing ledger immutability via Postgres Triggers.
	EnforceLedgerImmutability(db)
}

// EnforceLedgerImmutability creates database-level triggers to prevent any updates or deletions
// on the ledger entries, ensuring financial audit integrity.
func EnforceLedgerImmutability(db *gorm.DB) {
	// Function that throws an error whenever an update or delete is attempted.
	createFuncSQL := `
	CREATE OR REPLACE FUNCTION protect_ledger_immutability()
	RETURNS TRIGGER AS $$
	BEGIN
		RAISE EXCEPTION 'Ledger entries are immutable and cannot be modified or deleted.';
	END;
	$$ LANGUAGE plpgsql;`

	// Trigger to block UPDATE operations on the 'ledgers' table.
	createUpdateTriggerSQL := `
	DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'prevent_ledger_update') THEN
			CREATE TRIGGER prevent_ledger_update
			BEFORE UPDATE ON ledgers
			FOR EACH ROW EXECUTE FUNCTION protect_ledger_immutability();
		END IF;
	END $$;`

	// Trigger to block DELETE operations on the 'ledgers' table.
	createDeleteTriggerSQL := `
	DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'prevent_ledger_delete') THEN
			CREATE TRIGGER prevent_ledger_delete
			BEFORE DELETE ON ledgers
			FOR EACH ROW EXECUTE FUNCTION protect_ledger_immutability();
		END IF;
	END $$;`

	db.Exec(createFuncSQL)
	db.Exec(createUpdateTriggerSQL)
	db.Exec(createDeleteTriggerSQL)
}
