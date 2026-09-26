// Command seed inserts the two demo wallets used by the README and the smoke
// test. It is idempotent: existing wallets are left untouched.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"wallet/internal/config"
	"wallet/internal/constants"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx := context.Background()
	connectCtx, cancel := context.WithTimeout(ctx, constants.ConnectTimeout)
	conn, err := pgx.Connect(connectCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))

	if _, err := conn.Exec(ctx, constants.SeedWalletsSQL); err != nil {
		return err
	}

	rows, err := conn.Query(ctx, constants.ListSeedWalletsSQL)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, currency string
		var balance int64
		if err := rows.Scan(&id, &balance, &currency); err != nil {
			return err
		}
		fmt.Printf("%s: %d %s\n", id, balance, currency)
	}
	return rows.Err()
}
