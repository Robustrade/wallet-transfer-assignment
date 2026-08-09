package app

import (
	"context"
	"fmt"

	"github.com/chaitanya-bhagat/wallet-transfer-assignment/database"
	db "github.com/chaitanya-bhagat/wallet-transfer-assignment/database/generated"
	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/repository"
	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/services"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Dependencies struct {
	TransferService *services.Service
	db              *pgxpool.Pool
}

func NewDependencies(ctx context.Context, dbURL string) (*Dependencies, error) {

	pool, err := database.NewDBConnection(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	queries := db.New(pool)
	repo := repository.NewTransferRepo(pool, queries)
	transferSvc := services.NewTransferService(repo)

	return &Dependencies{
		TransferService: &transferSvc,
		db:              pool,
	}, nil
}

func (d *Dependencies) Close() {
	if d.db != nil {
		d.db.Close()
	}
}
