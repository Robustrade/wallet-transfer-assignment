
package service

import (
    "errors"

    "wallet-transfer-assignment/internal/domain"
    "wallet-transfer-assignment/internal/repository"

    "github.com/google/uuid"
    "github.com/jmoiron/sqlx"
)

type TransferService struct {
    db           *sqlx.DB
    walletRepo   *repository.WalletRepository
    transferRepo *repository.TransferRepository
    ledgerRepo   *repository.LedgerRepository
}

func NewTransferService(
    db *sqlx.DB,
    walletRepo *repository.WalletRepository,
    transferRepo *repository.TransferRepository,
    ledgerRepo *repository.LedgerRepository,
) *TransferService {

    return &TransferService{
        db: db,
        walletRepo: walletRepo,
        transferRepo: transferRepo,
        ledgerRepo: ledgerRepo,
    }
}

func (s *TransferService) CreateTransfer(
    idempotencyKey string,
    fromWalletID string,
    toWalletID string,
    amount int64,
) (*domain.Transfer, error) {

    existing, err := s.transferRepo.GetByIdempotencyKey(idempotencyKey)

    if err == nil {
        return existing, nil
    }

    tx, err := s.db.Beginx()
    if err != nil {
        return nil, err
    }

    defer tx.Rollback()

    var balance int64

    err = tx.Get(
        &balance,
        "SELECT balance FROM wallets WHERE id = $1 FOR UPDATE",
        fromWalletID,
    )

    if err != nil {
        return nil, err
    }

    if balance < amount {
        return nil, errors.New("insufficient balance")
    }

    _, err = tx.Exec(
        "UPDATE wallets SET balance = balance - $1 WHERE id = $2",
        amount,
        fromWalletID,
    )

    if err != nil {
        return nil, err
    }

    _, err = tx.Exec(
        "UPDATE wallets SET balance = balance + $1 WHERE id = $2",
        amount,
        toWalletID,
    )

    if err != nil {
        return nil, err
    }

    transfer := &domain.Transfer{
        ID: uuid.New().String(),
        IdempotencyKey: idempotencyKey,
        FromWalletID: fromWalletID,
        ToWalletID: toWalletID,
        Amount: amount,
        Status: "PROCESSED",
    }

    err = s.transferRepo.Create(tx, transfer)
    if err != nil {
        return nil, err
    }

    err = s.ledgerRepo.CreateEntry(
        tx,
        fromWalletID,
        transfer.ID,
        "DEBIT",
        amount,
    )

    if err != nil {
        return nil, err
    }

    err = s.ledgerRepo.CreateEntry(
        tx,
        toWalletID,
        transfer.ID,
        "CREDIT",
        amount,
    )

    if err != nil {
        return nil, err
    }

    err = tx.Commit()
    if err != nil {
        return nil, err
    }

    return transfer, nil
}
