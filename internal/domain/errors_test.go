package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
)

// The handler needs both facts from one error: that a wallet was missing, so it
// can return 404, and which one, so the message is useful.
func TestWalletNotFoundMatchesTheCategoryAndNamesTheWallet(t *testing.T) {
	var err error = &domain.WalletNotFoundError{WalletID: "wallet_9"}

	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatal("WalletNotFoundError does not match ErrWalletNotFound")
	}

	var notFound *domain.WalletNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatal("WalletNotFoundError could not be recovered with errors.As")
	}
	if notFound.WalletID != "wallet_9" {
		t.Fatalf("wallet id is %q, want wallet_9", notFound.WalletID)
	}
	if !strings.Contains(err.Error(), "wallet_9") {
		t.Fatalf("message %q does not name the wallet", err.Error())
	}
}

func TestWalletNotFoundDoesNotMatchUnrelatedErrors(t *testing.T) {
	var err error = &domain.WalletNotFoundError{WalletID: "wallet_9"}

	if errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatal("WalletNotFoundError matched ErrInsufficientFunds")
	}
}
