package domain

import (
	"errors"
	"time"
)

var (
	ErrWalletNotFound = errors.New("wallet not found")
)

type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
