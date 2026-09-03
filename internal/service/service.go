package service

import (
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

type Service struct {
	store *repository.Store
}

func New(store *repository.Store) *Service {
	return &Service{store: store}
}

func now() time.Time { return time.Now().UTC() }
