package controller

import (
	db "wallet-transfer-assigment/datasource"
	"wallet-transfer-assigment/mapper"
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
)

type Controller struct {
	db    db.DataSourceer
	log   utils.Logger
	maper mapper.Mapper
}
type Controllerer interface {
	walletController
	transactioner
}

func NewController(log utils.Logger) (Controllerer, error) {
	dataSource, err := db.NewDataSource(log)
	if err != nil {
		log.Error("Failed to initialise database", zap.Error(err))
		return nil, err
	}
	return &Controller{
		log:   log,
		db:    dataSource,
		maper: mapper.NewMapper(log),
	}, nil
}
