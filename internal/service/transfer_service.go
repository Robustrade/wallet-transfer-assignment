package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"wallet-assignment/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TransferService struct {
	db *gorm.DB
}

func NewTransferService(db *gorm.DB) *TransferService {
	return &TransferService{db}
}

func (s *TransferService) CreateTransfer(ctx context.Context, key, from, to string, amount int64) (string, error) {

	if key == "" || from == "" || to == "" {
		return "", errors.New("invalid input")
	}
	if amount <= 0 {
		return "", errors.New("amount must be greater than zero")
	}
	if from == to {
		return "", errors.New("cannot transfer to same wallet")
	}

	var existing domain.IdempotencyRecord
	if err := s.db.First(&existing, "key = ?", key).Error; err == nil {
		var resp map[string]string
		if err := json.Unmarshal([]byte(existing.Response), &resp); err != nil {
			return "", err
		}
		if id, ok := resp["transferId"]; ok {
			return id, nil
		}
		return "", errors.New("invalid idempotency response")
	}

	var transferID string

	err := s.db.Transaction(func(tx *gorm.DB) error {

		var fromWallet, toWallet domain.Wallet

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&fromWallet, "id = ?", from).Error; err != nil {
			return err
		}

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&toWallet, "id = ?", to).Error; err != nil {
			return err
		}

		if fromWallet.Balance < amount {
			return errors.New("insufficient funds")
		}

		fromWallet.Balance -= amount
		toWallet.Balance += amount

		if err := tx.Save(&fromWallet).Error; err != nil {
			return err
		}
		if err := tx.Save(&toWallet).Error; err != nil {
			return err
		}

		transfer := domain.Transfer{
			ID:           uuid.New().String(),
			FromWalletID: from,
			ToWalletID:   to,
			Amount:       amount,
			State:        domain.PROCESSED,
		}

		if err := tx.Create(&transfer).Error; err != nil {
			return err
		}

		if err := tx.Create(&domain.LedgerEntry{WalletID: from, Type: "DEBIT", Amount: amount, TransferID: transfer.ID}).Error; err != nil {
			return err
		}

		if err := tx.Create(&domain.LedgerEntry{WalletID: to, Type: "CREDIT", Amount: amount, TransferID: transfer.ID}).Error; err != nil {
			return err
		}

		resp := map[string]string{"transferId": transfer.ID}
		b, _ := json.Marshal(resp)

		if err := tx.Create(&domain.IdempotencyRecord{Key: key, Response: string(b)}).Error; err != nil {
			return err
		}

		transferID = transfer.ID

		log.Printf("transfer completed: %s -> %s amount=%d", from, to, amount)

		return nil
	})

	if err != nil {
		return "", err
	}

	return transferID, nil
}
