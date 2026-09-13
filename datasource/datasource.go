package datasource

import (
	"database/sql"
	db "wallet-transfer-assigment/internal/db/generated"
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

type DataSource struct {
	log     utils.Logger
	Queries *db.Queries
	db      *sql.DB
}
type DataSourceer interface {
	walletDBer
	transactioner
}

func NewDataSource(log utils.Logger) (DataSourceer, error) {
	con, err := sql.Open("sqlite", "wallet.db")
	if err != nil {
		log.Error("db initialisation failed", zap.Error(err))
		return nil, err
	}
	if err := con.Ping(); err != nil {
		log.Error("failed to connect to database", zap.Error(err))
		return nil, err
	}
	err = runMigrations(con, log)
	if err != nil {
		log.Error("migration Failed", zap.Error(err))
	}

	log.Info("Database connected successfully")
	return &DataSource{
		db:      con,
		log:     log,
		Queries: db.New(con),
	}, nil
}
