package datasource

import (
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
)

type DataSource struct {
	log *zap.Logger
}
type DataSourceer interface{}

func NewDataSource(log utils.Logger) DataSourceer {
	return &DataSource{
		log: log.Logs,
	}
}
