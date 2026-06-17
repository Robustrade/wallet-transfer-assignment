package transfer

import (
	"gorm.io/gorm"
)

type TransferRepo struct {
}

func (r *TransferRepo) Create(
	tx *gorm.DB,
	transfer Transfer,
) (*Transfer, error) {

	query := `
    INSERT INTO transfers (
        idempotency_key,
        from_wallet_id,
        to_wallet_id,
        amount,
        state
    )
    VALUES (?, ?, ?, ?, ?)
`

	err := tx.Exec(
		query,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		"PENDING",
	).Error

	if err != nil {
		return nil, err
	}

	// fmt.Println("inserted successfully...")
	var res Transfer
	err = tx.Where("idempotency_key = ?", transfer.IdempotencyKey).First(&res).Error
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *TransferRepo) UpdateState(
	tx *gorm.DB,
	transferID uint64,
	state string,
) error {

	query := `
        UPDATE transfers
        SET state = $1,
            updated_at = NOW()
        WHERE id = $2
    `

	err := tx.Exec(
		query,
		state,
		transferID,
	).Error

	return err
}
