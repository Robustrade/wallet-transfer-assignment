package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidRequest = errors.New("invalid wallet request")
	ErrNotFound       = errors.New("wallet not found")
)

type Wallet struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Balance   int64     `json:"balance"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type CreateRequest struct {
	Name           string `json:"name"`
	InitialBalance int64  `json:"initialBalance"`
	Currency       string `json:"currency"`
}

func (request CreateRequest) Validate() error {
	if strings.TrimSpace(request.Name) == "" || len(request.Name) > 255 || request.InitialBalance < 0 {
		return ErrInvalidRequest
	}
	return nil
}

type UpdateBalanceRequest struct {
	Balance int64 `json:"balance"`
}

type BalanceHistory struct {
	ID        int64     `json:"id"`
	WalletID  string    `json:"walletId"`
	Balance   int64     `json:"balance"`
	Change    int64     `json:"change"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}
