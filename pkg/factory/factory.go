package factory

import (
	"database/sql"
	"fmt"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/config"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
	"github.com/loopinnovates/wallet-transfer-assignment/repository/postgres"
)

type Factory struct {
	ReadPGReplica  *sql.DB
	WritePGReplica *sql.DB
}

func NewFactory(appConfig *config.Config) (*Factory, error) {
	pool := postgres.PoolConfig{
		MaxOpenConns:    appConfig.DBMaxOpenConns,
		MaxIdleConns:    appConfig.DBMaxIdleConns,
		ConnMaxLifetime: appConfig.DBConnMaxLifetime,
		ConnMaxIdleTime: appConfig.DBConnMaxIdleTime,
	}

	logger.Debug().Msg("connecting to read replica")
	readReplica, err := postgres.NewDB(appConfig.ReadPGURI, pool)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to connect to read replica")
		return nil, fmt.Errorf("connecting to read replica: %w", err)
	}

	logger.Debug().Msg("connecting to write replica")
	writeReplica, err := postgres.NewDB(appConfig.WritePGURI, pool)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to connect to write replica")
		err2 := readReplica.Close()
		if err2 != nil {
			logger.Error().Err(err2).Msg("error closing read replica")
		}
		return nil, fmt.Errorf("connecting to write replica: %w", err)
	}

	logger.Info().Msg("connected to read and write replicas")
	return &Factory{
		ReadPGReplica:  readReplica,
		WritePGReplica: writeReplica,
	}, nil
}

func (f *Factory) Close() {
	logger.Debug().Msg("closing db connections")
	if f.ReadPGReplica != nil {
		err := f.ReadPGReplica.Close()
		if err != nil {
			logger.Error().Err(err).Msg("error closing read replica")
		}
	}
	if f.WritePGReplica != nil {
		err := f.WritePGReplica.Close()
		if err != nil {
			logger.Error().Err(err).Msg("error closing write replica")
		}
	}
}
