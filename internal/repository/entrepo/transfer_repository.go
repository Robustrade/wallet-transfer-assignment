package entrepo

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"wallet-transfer-assignment/ent"
	"wallet-transfer-assignment/ent/transfer"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type TransferRepository struct {
	client *ent.Client
}

func NewTransferRepository(client *ent.Client) *TransferRepository {
	return &TransferRepository{
		client: client,
	}
}

func (r *TransferRepository) FindByIdempotencyKey(
	ctx context.Context,
	key string,
) (*domain.Transfer, error) {
	t, err := r.client.Transfer.
		Query().
		Where(transfer.IdempotencyKeyEQ(key)).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, repository.ErrNotFound
		}

		return nil, err
	}

	return toDomainTransfer(t), nil
}

func (r *TransferRepository) Create(
	ctx context.Context,
	t *domain.Transfer,
) error {
	created, err := r.client.Transfer.
		Create().
		SetID(t.ID).
		SetIdempotencyKey(t.IdempotencyKey).
		SetFromWalletID(t.FromWalletID).
		SetToWalletID(t.ToWalletID).
		SetAmount(t.Amount).
		SetStatus(transfer.Status(t.Status)).
		SetFailureReason(
			transfer.FailureReason(t.FailureReason),
		).
		Save(ctx)

	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" {
			return repository.ErrAlreadyExists
		}

		return err
	}

	t.CreatedAt = created.CreatedAt
	t.UpdatedAt = created.UpdatedAt

	return nil
}

func (r *TransferRepository) UpdateStatus(
	ctx context.Context,
	id uuid.UUID,
	status domain.TransferStatus,
	failureReason domain.TransferFailureReason,
) error {

	if status != domain.TransferStatusProcessed &&
		status != domain.TransferStatusFailed {
		return domain.ErrInvalidTransferState
	}

	affected, err := r.client.Transfer.
		Update().
		Where(
			transfer.IDEQ(id),
			transfer.StatusEQ(
				transfer.Status(domain.TransferStatusPending),
			),
		).
		SetStatus(
			transfer.Status(status),
		).
		SetFailureReason(
			transfer.FailureReason(failureReason),
		).
		Save(ctx)

	if err != nil {
		return err
	}

	if affected == 0 {
		return domain.ErrInvalidTransferState
	}

	return nil
}

func toDomainTransfer(t *ent.Transfer) *domain.Transfer {
	return &domain.Transfer{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         domain.TransferStatus(t.Status),
		FailureReason: domain.TransferFailureReason(
			t.FailureReason,
		),
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}
