package db

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq"
	"os"
)

var DB *sql.DB

func InitDB() {
	connStr := os.Getenv("DB_URL")

	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatal(err)
	}
}

func InitSchema() {
	query := `
	CREATE TABLE IF NOT EXISTS wallets (
		id TEXT PRIMARY KEY,
		balance BIGINT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS transfers (
		id TEXT PRIMARY KEY,
		from_wallet_id TEXT,
		to_wallet_id TEXT,
		amount BIGINT,
		status TEXT,
		created_at TIMESTAMP DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS ledger_entries (
		id SERIAL PRIMARY KEY,
		wallet_id TEXT,
		transfer_id TEXT,
		type TEXT,
		amount BIGINT,
		created_at TIMESTAMP DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS idempotency_records (
		idempotency_key TEXT PRIMARY KEY,
		transfer_id TEXT,
		response JSONB,
		created_at TIMESTAMP DEFAULT now()
	);
	`

	_, err := DB.Exec(query)
	if err != nil {
		log.Fatal(err)
	}

	DB.Exec(`
		DO $$
		BEGIN
			-- failure_reason column
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name='transfers' AND column_name='failure_reason'
			) THEN
				ALTER TABLE transfers ADD COLUMN failure_reason TEXT;
			END IF;

			-- fk_from_wallet
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.table_constraints
				WHERE constraint_name='fk_from_wallet'
			) THEN
				ALTER TABLE transfers
				ADD CONSTRAINT fk_from_wallet
				FOREIGN KEY (from_wallet_id) REFERENCES wallets(id);
			END IF;

			-- fk_to_wallet
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.table_constraints
				WHERE constraint_name='fk_to_wallet'
			) THEN
				ALTER TABLE transfers
				ADD CONSTRAINT fk_to_wallet
				FOREIGN KEY (to_wallet_id) REFERENCES wallets(id);
			END IF;

			-- wallet balance check
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.table_constraints
				WHERE constraint_name='balance_non_negative'
			) THEN
				ALTER TABLE wallets
				ADD CONSTRAINT balance_non_negative
				CHECK (balance >= 0);
			END IF;

			-- amount check
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.table_constraints
				WHERE constraint_name='amount_positive'
			) THEN
				ALTER TABLE transfers
				ADD CONSTRAINT amount_positive
				CHECK (amount > 0);
			END IF;

			-- status check
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.table_constraints
				WHERE constraint_name='valid_status'
			) THEN
				ALTER TABLE transfers
				ADD CONSTRAINT valid_status
				CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'));
			END IF;

		END $$;
	`)

	DB.Exec(`
		INSERT INTO wallets (id, balance)
		VALUES ('wallet_1', 1000), ('wallet_2', 500)
		ON CONFLICT (id) DO NOTHING;
	`)
}
