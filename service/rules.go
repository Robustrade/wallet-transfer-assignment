package service

import (
	"github.com/Robustrade/wallet-transfer-assignment/helpers"
	"github.com/Robustrade/wallet-transfer-assignment/model"
)

// Business-rule helpers for transfers. Repositories only run SQL; these checks
// live here so every caller of the service (HTTP or not) gets the same rules.

// validateTransferInput normalizes ids and enforces:
//   - non-empty wallet ids
//   - from != to (no same-account transfer)
//   - amount > 0
func validateTransferInput(fromWalletID, toWalletID string, amount int64) (from, to string, amt helpers.Money, err error) {
	from, err = model.NormalizeID(fromWalletID)
	if err != nil {
		return "", "", 0, err
	}
	to, err = model.NormalizeID(toWalletID)
	if err != nil {
		return "", "", 0, err
	}
	if err := requireDistinctWallets(from, to); err != nil {
		return "", "", 0, err
	}
	amt, err = requirePositiveAmount(amount)
	if err != nil {
		return "", "", 0, err
	}
	return from, to, amt, nil
}

func requirePositiveAmount(amount int64) (helpers.Money, error) {
	return model.NewAmount(amount) // rejects 0 and negatives
}

func requireDistinctWallets(from, to string) error {
	if from == to {
		return model.ErrSameWallet
	}
	return nil
}

// assertTransferReadyToPersist re-checks transfer invariants right before INSERT.
func assertTransferReadyToPersist(tr *model.Transfer) error {
	if tr == nil {
		return ErrInvariantBroken
	}
	if err := requireDistinctWallets(tr.FromWalletID, tr.ToWalletID); err != nil {
		return err
	}
	if tr.Amount <= 0 {
		return model.ErrInvalidAmount
	}
	return nil
}

// assertAmountForBalanceUpdate guards Debit/Credit so balances never move by a non-positive amount.
func assertAmountForBalanceUpdate(amount helpers.Money) error {
	if amount <= 0 {
		return model.ErrInvalidAmount
	}
	return nil
}

// assertConsistentLedgerPair enforces double-entry shape before InsertPair:
// one DEBIT on from and one CREDIT on to, same transfer id, same positive amount.
func assertConsistentLedgerPair(tr *model.Transfer, debit, credit *model.LedgerEntry) error {
	if tr == nil {
		return model.ErrLedgerRequiresTransfer
	}
	if debit == nil || credit == nil {
		return model.ErrLedgerRequiresTransfer
	}
	if tr.Status != model.StatusProcessed {
		return model.ErrLedgerRequiresTransfer
	}
	if debit.EntryType != model.EntryDebit || credit.EntryType != model.EntryCredit {
		return model.ErrInvalidEntryType
	}
	if debit.TransferID != credit.TransferID {
		return model.ErrLedgerRequiresTransfer
	}
	if debit.TransferID != tr.ID || credit.TransferID != tr.ID {
		return model.ErrLedgerRequiresTransfer
	}
	if err := requireDistinctWallets(debit.WalletID, credit.WalletID); err != nil {
		return err
	}
	if debit.WalletID != tr.FromWalletID || credit.WalletID != tr.ToWalletID {
		return ErrInvariantBroken
	}
	if debit.Amount <= 0 || credit.Amount <= 0 {
		return model.ErrInvalidAmount
	}
	if debit.Amount != credit.Amount || debit.Amount != tr.Amount {
		return model.ErrInvalidAmount
	}
	return nil
}
