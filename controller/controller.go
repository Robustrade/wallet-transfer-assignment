package controller

import (
	db "wallet-transfer-assigment/datasource"
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
)

type Controller struct {
	db  db.DataSourceer
	log *zap.Logger
}
type Controllerer interface{}

func NewController(log utils.Logger) Controllerer {
	return &Controller{
		log: log.Logs,
		db:  db.NewDataSource(log),
	}
}
