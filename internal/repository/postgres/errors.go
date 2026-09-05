package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"wallet-transfer/internal/domain"
)

const (
	codeUniqueViolation      = "23505"
	codeForeignKeyViolation  = "23503"
	codeCheckViolation       = "23514"
	codeSerializationFailure = "40001"
	codeDeadlockDetected     = "40P01"
	codeLockNotAvailable     = "55P03"
	codeQueryCanceled        = "57014"
)

const (
	constraintBalanceNonNegative = "wallets_balance_check"
	constraintLedgerOnePerSide   = "ledger_entries_one_per_side"
	constraintIdempotencyKey     = "idempotency_records_pkey"
)

func translateError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch pgErr.Code {
	case codeCheckViolation:
		if pgErr.ConstraintName == constraintBalanceNonNegative {
			return fmt.Errorf("%w: %s", domain.ErrInsufficientFunds, pgErr.ConstraintName)
		}
		return fmt.Errorf("%w: %s", domain.ErrValidation, pgErr.ConstraintName)

	case codeUniqueViolation:
		switch pgErr.ConstraintName {
		case constraintIdempotencyKey:
			return domain.ErrIdempotencyKeyAlreadyUsed
		case constraintLedgerOnePerSide:
			return fmt.Errorf("%w: ledger entry already recorded", domain.ErrInvalidStateTransition)
		}
		return fmt.Errorf("%w: %s", domain.ErrInvalidStateTransition, pgErr.ConstraintName)

	case codeForeignKeyViolation:
		return fmt.Errorf("%w: %s", domain.ErrWalletNotFound, pgErr.ConstraintName)

	case codeDeadlockDetected, codeSerializationFailure, codeLockNotAvailable:
		return fmt.Errorf("%w: %s", domain.ErrConflict, pgErr.Message)

	case codeQueryCanceled:
		return fmt.Errorf("%w: statement canceled", domain.ErrTimeout)
	}

	return err
}
