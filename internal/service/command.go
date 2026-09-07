package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

// DefaultCurrency is used when a request omits an explicit currency.
const DefaultCurrency = "USD"

// CreateTransferCommand is the validated input to TransferService.CreateTransfer.
type CreateTransferCommand struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Currency       string
}

// normalized returns a copy with defaults applied and whitespace trimmed.
func (c CreateTransferCommand) normalized() CreateTransferCommand {
	c.IdempotencyKey = strings.TrimSpace(c.IdempotencyKey)
	c.FromWalletID = strings.TrimSpace(c.FromWalletID)
	c.ToWalletID = strings.TrimSpace(c.ToWalletID)
	c.Currency = strings.TrimSpace(c.Currency)
	if c.Currency == "" {
		c.Currency = DefaultCurrency
	}
	return c
}

// validate checks the command in isolation (no I/O).
func (c CreateTransferCommand) validate() error {
	switch {
	case c.IdempotencyKey == "":
		return fmt.Errorf("%w: idempotencyKey is required", domain.ErrValidation)
	case c.FromWalletID == "":
		return fmt.Errorf("%w: fromWalletId is required", domain.ErrValidation)
	case c.ToWalletID == "":
		return fmt.Errorf("%w: toWalletId is required", domain.ErrValidation)
	case c.FromWalletID == c.ToWalletID:
		return fmt.Errorf("%w: fromWalletId and toWalletId must differ", domain.ErrValidation)
	case c.Amount <= 0:
		return fmt.Errorf("%w: amount must be a positive integer", domain.ErrValidation)
	}
	return nil
}

// money builds the domain Money value for the command.
func (c CreateTransferCommand) money() (domain.Money, error) {
	return domain.NewMoney(c.Amount, c.Currency)
}

// requestHash is a stable fingerprint of the transfer-defining fields. A reused
// idempotency key whose hash does not match a stored record is a conflict, not a
// safe replay.
func (c CreateTransferCommand) requestHash() string {
	var b strings.Builder
	b.WriteString(c.FromWalletID)
	b.WriteByte('|')
	b.WriteString(c.ToWalletID)
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(c.Amount, 10))
	b.WriteByte('|')
	b.WriteString(c.Currency)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
