package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
)

func TestNewLedgerPair(t *testing.T) {
	transferID := uuid.New()
	fromWalletID := "wallet_1"
	toWalletID := "wallet_2"
	amount := int64(100)

	debit, credit := domain.NewLedgerPair(transferID, fromWalletID, toWalletID, amount)
	if debit.TransferID != transferID || credit.TransferID != transferID {
		t.Errorf("Expected transfer ID %s for both entries, got %s and %s", transferID, debit.TransferID, credit.TransferID)
	}
	if debit.WalletID != fromWalletID || credit.WalletID != toWalletID {
		t.Errorf("Expected wallet IDs %s and %s, got %s and %s", fromWalletID, toWalletID, debit.WalletID, credit.WalletID)
	}
	if debit.Type != domain.EntryDebit || credit.Type != domain.EntryCredit {
		t.Errorf("Expected types DEBIT and CREDIT, got %s and %s", debit.Type, credit.Type)
	}
	if debit.Amount != amount || credit.Amount != amount {
		t.Errorf("Expected amount %d for both entries, got %d and %d", amount, debit.Amount, credit.Amount)
	}
	if !debit.CreatedAt.Equal(credit.CreatedAt) {
		t.Errorf("Expected same CreatedAt for both entries, got %v and %v", debit.CreatedAt, credit.CreatedAt)
	}

}
