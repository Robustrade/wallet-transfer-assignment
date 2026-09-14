package datasource

import (
	"database/sql"
	db "wallet-transfer-assigment/internal/db/generated"
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

const productionSQLiteDSN = "file:wallet.db?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"

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
	con, err := sql.Open("sqlite", productionSQLiteDSN)
	if err != nil {
		log.Error("db initialisation failed", zap.Error(err))
		return nil, err
	}
	// SQLite permits one writer at a time. Serializing connections in this process
	// avoids self-inflicted lock contention; busy_timeout handles other processes.
	con.SetMaxOpenConns(1)
	con.SetMaxIdleConns(1)
	if err := con.Ping(); err != nil {
		log.Error("failed to connect to database", zap.Error(err))
		return nil, err
	}
	err = runMigrations(con, log)
	if err != nil {
		log.Error("migration Failed", zap.Error(err))
		_ = con.Close()
		return nil, err
	}

	log.Info("Database connected successfully")
	return &DataSource{
		db:      con,
		log:     log,
		Queries: db.New(con),
	}, nil
}
