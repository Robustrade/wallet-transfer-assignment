package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/infra"
	"wallet-transfer-assignment/internal/repository/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config := infra.LoadConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := postgres.Connect(ctx, config.DatabaseURL)
	if err != nil {
		logger.Error("connect_database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	transferImbalances, err := countTransferImbalances(ctx, pool)
	if err != nil {
		logger.Error("check_transfer_ledger_invariant", "error", err)
		os.Exit(1)
	}
	walletMismatches, err := countWalletBalanceMismatches(ctx, pool)
	if err != nil {
		logger.Error("check_wallet_balance_invariant", "error", err)
		os.Exit(1)
	}

	logger.Info("invariant_check_complete",
		"transfer_imbalances", transferImbalances,
		"wallet_balance_mismatches", walletMismatches,
	)
	if transferImbalances != 0 || walletMismatches != 0 {
		os.Exit(1)
	}
}

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func countTransferImbalances(ctx context.Context, db queryer) (int, error) {
	var count int
	err := db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM (
			SELECT t.id
			FROM transfers t
			LEFT JOIN ledger_entries le ON le.transfer_id = t.id
			WHERE t.status = 'PROCESSED'
			GROUP BY t.id
			HAVING COUNT(le.id) <> 2
			    OR COALESCE(SUM(CASE WHEN le.entry_type = 'DEBIT' THEN le.amount ELSE 0 END), 0)
			     <> COALESCE(SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount ELSE 0 END), 0)
		) imbalanced
	`).Scan(&count)
	return count, err
}

func countWalletBalanceMismatches(ctx context.Context, db queryer) (int, error) {
	var count int
	err := db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM wallets w
		WHERE w.balance <> w.opening_balance + COALESCE((
			SELECT SUM(CASE
				WHEN le.entry_type = 'CREDIT' THEN le.amount
				WHEN le.entry_type = 'DEBIT' THEN -le.amount
				ELSE 0
			END)
			FROM ledger_entries le
			JOIN transfers t ON t.id = le.transfer_id
			WHERE le.wallet_id = w.id
			  AND t.status = 'PROCESSED'
		), 0)
	`).Scan(&count)
	return count, err
}
