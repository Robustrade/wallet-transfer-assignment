package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/priiyank-xd/wallet-transfer-service/internal/domain"
	"github.com/priiyank-xd/wallet-transfer-service/internal/repository"
)

type TransferService struct {
	repo *repository.Repository
}

func NewTransferService(repo *repository.Repository) *TransferService {
	return &TransferService{repo: repo}
}

func (s *TransferService) CreateTransfer(ctx context.Context, req domain.TransferRequest) (domain.TransferResponse, int, error) {
	var resp domain.TransferResponse

	if err := req.Validate(); err != nil {
		return resp, http.StatusBadRequest, err
	}

	stored, status, ok, err := s.checkIdemPotencyRecord(ctx, req.IdempotencyKey)
	if err != nil {
		return resp, http.StatusInternalServerError, err
	}
	if ok {
		return stored, status, nil
	}

	return s.processTransfer(ctx, req)
}

func (s *TransferService) checkIdemPotencyRecord(ctx context.Context, key string) (domain.TransferResponse, int, bool, error) {
	body, status, err := s.repo.FindIdempotencyRecord(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.TransferResponse{}, 0, false, nil
	}
	if err != nil {
		return domain.TransferResponse{}, 0, false, err
	}

	var resp domain.TransferResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return domain.TransferResponse{}, http.StatusInternalServerError, true, err
	}
	return resp, status, true, nil
}

func (s *TransferService) loadStoredResponse(ctx context.Context, key string) (domain.TransferResponse, int, error) {
	if resp, status, ok, err := s.checkIdemPotencyRecord(ctx, key); err != nil {
		return domain.TransferResponse{}, 0, err
	} else if ok {
		return resp, status, nil
	}

	t, err := s.repo.FindByIdempotencyKey(ctx, key)
	if err != nil {
		return domain.TransferResponse{}, 0, err
	}

	resp := responseFromTransfer(*t, t.Status, "")
	switch t.Status {
	case domain.PROCESSED:
		return resp, http.StatusCreated, nil
	case domain.FAILED:
		resp.Error = domain.ErrInsufficientFunds.Error()
		return resp, http.StatusConflict, domain.ErrInsufficientFunds
	default:
		return domain.TransferResponse{}, 0, domain.ErrIdempotencyConflict
	}
}

func responseFromTransfer(transfer domain.Transfer, status domain.TransferStatus, errMsg string) domain.TransferResponse {
	return domain.TransferResponse{
		TransferID:     transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		Status:         status,
		Error:          errMsg,
	}
}

func (s *TransferService) processTransfer(ctx context.Context, req domain.TransferRequest) (domain.TransferResponse, int, error) {
	var (
		resp   domain.TransferResponse
		status int
		bizErr error
	)

	err := s.repo.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if r, st, ok, checkErr := s.checkIdemPotencyRecord(ctx, req.IdempotencyKey); checkErr != nil {
			return checkErr
		} else if ok {
			resp, status = r, st
			return nil
		}

		//create new transfer
		transfer := newPendingTransfer(req)
		if err := s.repo.CreateTransfer(ctx, tx, transfer); err != nil {
			if errors.Is(err, repository.ErrDuplicateIdempotency) {
				stored, st, loadErr := s.loadStoredResponse(ctx, req.IdempotencyKey)
				resp, status = stored, st
				if errors.Is(loadErr, domain.ErrInsufficientFunds) {
					bizErr = loadErr
					return nil
				}
				return loadErr
			}
			return err
		}

		//Lock wallet
		source, _, err := s.lockWalletsInOrder(ctx, tx, transfer.FromWalletID, transfer.ToWalletID)
		if err != nil {
			return err
		}

		//Insufficient balance
		if source.Balance < transfer.Amount {
			r, st, err := s.abortTransfer(ctx, tx, transfer)
			resp, status, bizErr = r, st, domain.ErrInsufficientFunds
			return err
		}

		//Update Status and create ledger
		done, st, err := s.completeTransfer(ctx, tx, transfer)
		resp, status = done, st
		return err
	})

	if err != nil {
		return resp, http.StatusInternalServerError, err
	}
	if bizErr != nil {
		return resp, status, bizErr
	}
	return resp, status, nil
}

func newPendingTransfer(req domain.TransferRequest) domain.Transfer {
	now := time.Now()
	return domain.Transfer{
		ID:             uuid.NewString(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.PENDING,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func (s *TransferService) lockWalletsInOrder(ctx context.Context, tx pgx.Tx, fromID, toID string) (source, dest *domain.Wallet, err error) {
	firstID, secondID, fromIsFirst := fromID, toID, true
	if secondID < firstID {
		firstID, secondID, fromIsFirst = secondID, firstID, false
	}

	w1, err := s.repo.GetWalletWithLock(ctx, tx, firstID)
	if err != nil {
		return nil, nil, err
	}
	w2, err := s.repo.GetWalletWithLock(ctx, tx, secondID)
	if err != nil {
		return nil, nil, err
	}

	if fromIsFirst {
		return w1, w2, nil
	}
	return w2, w1, nil
}

func (s *TransferService) abortTransfer(ctx context.Context, tx pgx.Tx, transfer domain.Transfer) (domain.TransferResponse, int, error) {
	err := s.repo.UpdateTransferStatus(ctx, tx, transfer.ID, domain.FAILED)
	if err != nil {
		return domain.TransferResponse{}, 0, err
	}

	resp := responseFromTransfer(transfer, domain.FAILED, domain.ErrInsufficientFunds.Error())
	body, _ := json.Marshal(resp)
	if err := s.repo.SaveIdempotencyRecord(ctx, tx, transfer.IdempotencyKey, transfer.ID, body, http.StatusConflict); err != nil {
		return domain.TransferResponse{}, 0, err
	}

	return resp, http.StatusConflict, nil
}

func (s *TransferService) completeTransfer(ctx context.Context, tx pgx.Tx, transfer domain.Transfer) (domain.TransferResponse, int, error) {
	if err := s.repo.UpdateBalance(ctx, tx, transfer.FromWalletID, -transfer.Amount); err != nil {
		return domain.TransferResponse{}, 0, err
	}
	if err := s.repo.UpdateBalance(ctx, tx, transfer.ToWalletID, transfer.Amount); err != nil {
		return domain.TransferResponse{}, 0, err
	}

	entries := []domain.LedgerEntry{
		{TransferID: transfer.ID, WalletID: transfer.FromWalletID, EntryType: domain.DEBIT, Amount: transfer.Amount},
		{TransferID: transfer.ID, WalletID: transfer.ToWalletID, EntryType: domain.CREDIT, Amount: transfer.Amount},
	}
	if err := s.repo.InsertLedgerEntries(ctx, tx, entries); err != nil {
		return domain.TransferResponse{}, 0, err
	}

	if err := s.repo.UpdateTransferStatus(ctx, tx, transfer.ID, domain.PROCESSED); err != nil {
		return domain.TransferResponse{}, 0, err
	}

	resp := responseFromTransfer(transfer, domain.PROCESSED, "")
	body, _ := json.Marshal(resp)
	if err := s.repo.SaveIdempotencyRecord(ctx, tx, transfer.IdempotencyKey, transfer.ID, body, http.StatusCreated); err != nil {
		return domain.TransferResponse{}, 0, err
	}

	return resp, http.StatusCreated, nil
}
