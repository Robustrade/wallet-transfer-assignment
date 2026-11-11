package postgres

import (
	"errors"
	"fmt"
	"time"
	"wallet-transfer-assignment/internal/appErrors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"wallet-transfer-assignment/internal/domain"
)

func SetTransactionTimeouts(tx *gorm.DB, lockTimeout, statementTimeout time.Duration) error {
	lockMS := max(int64(1), lockTimeout.Milliseconds())
	statementMS := max(int64(1), statementTimeout.Milliseconds())
	return tx.Exec(
		"SELECT set_config('lock_timeout', ?, true), set_config('statement_timeout', ?, true)",
		fmt.Sprintf("%dms", lockMS),
		fmt.Sprintf("%dms", statementMS),
	).Error
}

func LockTransfer(tx *gorm.DB, id string) (domain.Transfer, error) {
	return FindTransfer(tx.Clauses(clause.Locking{Strength: "UPDATE"}), id)
}

func FindTransfer(db *gorm.DB, id string) (domain.Transfer, error) {
	var transfer domain.Transfer
	err := db.Take(&transfer, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return transfer, appErrors.ErrTransferNotFound
	}
	return transfer, err
}

func LockWallet(tx *gorm.DB, id string) (domain.Wallet, error) {
	var wallet domain.Wallet
	err := tx.Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&wallet, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return wallet, appErrors.ErrWalletNotFound
	}
	return wallet, err
}

func UpdateWalletBalance(tx *gorm.DB, id string, balance int64) error {
	result := tx.Model(&domain.Wallet{}).Where("id = ?", id).UpdateColumns(map[string]any{
		"balance":    balance,
		"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
	})
	return requireRows(result, 1, "update wallet balance")
}

func InsertLedgerEntries(tx *gorm.DB, entries []domain.LedgerEntry) error {
	return requireRows(tx.Create(&entries), int64(len(entries)), "insert ledger entries")
}

func CompleteTransfer(tx *gorm.DB, transfer domain.Transfer, status int, body []byte) error {
	var reason any
	if transfer.FailureReason != "" {
		reason = transfer.FailureReason
	}
	result := tx.Model(&domain.Transfer{}).Where("id = ? AND status = ?", transfer.ID, domain.Pending).UpdateColumns(map[string]any{
		"status":         transfer.Status,
		"failure_reason": reason,
		"updated_at":     gorm.Expr("CURRENT_TIMESTAMP"),
	})
	if err := requireRows(result, 1, "complete transfer"); err != nil {
		return err
	}
	result = tx.Table("idempotency_records").Where("transfer_id = ? AND response_status IS NULL", transfer.ID).Updates(map[string]any{
		"response_status": status,
		"response_body":   string(body),
		"updated_at":      gorm.Expr("CURRENT_TIMESTAMP"),
	})
	return requireRows(result, 1, "save terminal response")
}

func LoadTransferResponse(tx *gorm.DB, id string) (int, []byte, error) {
	var response struct {
		ResponseStatus *int
		ResponseBody   *string
	}
	err := tx.Table("idempotency_records").Select("response_status,response_body").Where("transfer_id = ?", id).Take(&response).Error
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %w", appErrors.ErrTerminalResponseMissing, err)
	}
	if response.ResponseStatus == nil || response.ResponseBody == nil {
		return 0, nil, appErrors.ErrTerminalResponseMissing
	}
	return *response.ResponseStatus, []byte(*response.ResponseBody), nil
}

func requireRows(result *gorm.DB, want int64, operation string) error {
	if result.Error != nil {
		return fmt.Errorf("%s: %w", operation, result.Error)
	}
	if result.RowsAffected != want {
		return fmt.Errorf("%w: %s", appErrors.ErrUnexpectedRowCount, operation)
	}
	return nil
}
