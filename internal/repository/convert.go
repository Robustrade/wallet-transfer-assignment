package repository

import (
	"math/big"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/benjamin-benny/wallet-transfer/internal/domain"
)

// numericToAmount converts a scanned pgtype.Numeric to domain.Amount.
func numericToAmount(n pgtype.Numeric) domain.Amount {
	if !n.Valid || n.Int == nil {
		return domain.Amount("0.0000")
	}
	r := new(big.Rat).SetInt(n.Int)
	if n.Exp > 0 {
		mul := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n.Exp)), nil)
		r.Mul(r, new(big.Rat).SetInt(mul))
	} else if n.Exp < 0 {
		div := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-n.Exp)), nil)
		r.Quo(r, new(big.Rat).SetInt(div))
	}
	return domain.RatToAmount(r)
}

// amountToNumeric converts a domain.Amount to pgtype.Numeric for DB writes.
func amountToNumeric(a domain.Amount) pgtype.Numeric {
	var n pgtype.Numeric
	// pgtype.Numeric.Scan accepts a string representation of a decimal number.
	_ = n.Scan(a.String())
	return n
}
