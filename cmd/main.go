package main

import (
	"context"
	"fmt"
	"log"

	"github.com/chaitanya-bhagat/wallet-transfer-assignment/adapter/http/app"
	"github.com/chaitanya-bhagat/wallet-transfer-assignment/pkg/config"
)

func main() {
	ctx := context.Background()

	config, err := config.LoadPostgresConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	connString := fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=disable",
		config.User,
		config.Password,
		config.Host,
		config.Port,
		config.DB,
	)

	deps, err := app.NewDependencies(ctx, connString)
	if err != nil {
		log.Fatal(err)
	}
	defer deps.Close()
	app.NewTransferApp(deps).Start()
}
