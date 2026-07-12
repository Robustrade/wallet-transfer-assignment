package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/logging"
	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"gorm.io/gorm"
)

// CreateTransferRequest contains the data required to execute a wallet transfer.
type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// TransferResponse is returned by the transfer service after transfer execution.
type TransferResponse struct {
	TransferID        string              `json:"transferId"`
	FromWalletID      string              `json:"fromWalletId"`
	ToWalletID        string              `json:"toWalletId"`
	Amount            int64               `json:"amount"`
	State             model.TransferState `json:"state"`
	FromWalletBalance int64               `json:"fromWalletBalance"`
	ToWalletBalance   int64               `json:"toWalletBalance"`
	CreatedAt         time.Time           `json:"createdAt"`
}

var (
	ErrInvalidTransferRequest = errors.New("invalid transfer request")
	ErrInsufficientFunds      = errors.New("insufficient funds")
	ErrWalletNotFound         = errors.New("wallet not found")

	// errIdempotencyConflict signals that a concurrent duplicate request committed
	// first. It is handled internally to replay the stored result and is never
	// surfaced to callers.
	errIdempotencyConflict = errors.New("idempotency conflict")
)

// txFunc executes fn inside a database transaction. It is injectable so the
// business logic in CreateTransfer can be unit tested without a live database.
type txFunc func(ctx context.Context, fn func(tx *gorm.DB) error) error

// TransferService defines the service boundary for transfer business logic.
type TransferService interface {
	CreateTransfer(ctx context.Context, request CreateTransferRequest) (TransferResponse, error)
}

// transferService is the default implementation of TransferService.
type transferService struct {
	withTx          txFunc
	walletRepo      repository.WalletRepository
	transferRepo    repository.TransferRepository
	ledgerRepo      repository.LedgerRepository
	idempotencyRepo repository.IdempotencyRepository
	logger          *logging.Logger
}

// NewTransferService builds a TransferService using repository abstractions.
func NewTransferService(
	db *gorm.DB,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
	idempotencyRepo repository.IdempotencyRepository,
	logger *logging.Logger,
) TransferService {
	return &transferService{
		withTx: func(ctx context.Context, fn func(tx *gorm.DB) error) error {
			return db.WithContext(ctx).Transaction(fn)
		},
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		ledgerRepo:      ledgerRepo,
		idempotencyRepo: idempotencyRepo,
		logger:          logger,
	}
}

// CreateTransfer validates the request, enforces idempotency, and moves money
// atomically between two wallets while recording a double-entry ledger.
func (s *transferService) CreateTransfer(ctx context.Context, request CreateTransferRequest) (TransferResponse, error) {
	if request.IdempotencyKey == "" || request.FromWalletID == "" || request.ToWalletID == "" || request.Amount <= 0 {
		return TransferResponse{}, ErrInvalidTransferRequest
	}
	if request.FromWalletID == request.ToWalletID {
		return TransferResponse{}, ErrInvalidTransferRequest
	}

	var response TransferResponse
	var businessErr error

	err := s.withTx(ctx, func(tx *gorm.DB) error {
		// 1. Replay: if we have already processed this key, return the stored
		//    original result verbatim (exactly-once semantics).
		if existing, lookupErr := s.idempotencyRepo.GetByKey(ctx, tx, request.IdempotencyKey); lookupErr == nil {
			stored, decodeErr := decodeResponse(existing.Response)
			if decodeErr != nil {
				return decodeErr
			}
			response = stored
			businessErr = businessErrorFor(stored.State)
			return nil
		} else if !errors.Is(lookupErr, repository.ErrNotFound) {
			return lookupErr
		}

		// 2. Lock both wallets FOR UPDATE in a deterministic order to prevent
		//    deadlocks between opposing concurrent transfers (A->B and B->A).
		fromWallet, toWallet, err := s.lockWalletPair(ctx, tx, request.FromWalletID, request.ToWalletID)
		if err != nil {
			return err
		}

		transfer := model.Transfer{
			TransferID:   model.NewID(),
			FromWalletID: request.FromWalletID,
			ToWalletID:   request.ToWalletID,
			Amount:       request.Amount,
			State:        model.TransferStatePending,
		}
		if err := s.transferRepo.CreateTransfer(ctx, tx, transfer); err != nil {
			return err
		}

		// 3. Insufficient funds is a terminal FAILED outcome. It is still recorded
		//    against the idempotency key so retries return the same result.
		if fromWallet.Balance < request.Amount {
			if err := s.transferRepo.UpdateTransferState(ctx, tx, transfer.TransferID, model.TransferStateFailed); err != nil {
				return err
			}
			transfer.State = model.TransferStateFailed
			resp := buildResponse(&transfer, fromWallet.Balance, toWallet.Balance)
			if err := s.persistIdempotency(ctx, tx, request.IdempotencyKey, transfer.TransferID, resp); err != nil {
				return err
			}
			response = resp
			businessErr = ErrInsufficientFunds
			return nil
		}

		// 4. Move the money and record the double-entry ledger.
		newFrom := fromWallet.Balance - request.Amount
		newTo := toWallet.Balance + request.Amount
		if err := s.walletRepo.UpdateBalance(ctx, tx, request.FromWalletID, newFrom); err != nil {
			return err
		}
		if err := s.walletRepo.UpdateBalance(ctx, tx, request.ToWalletID, newTo); err != nil {
			return err
		}

		entries := []model.LedgerEntry{
			{
				LedgerEntryID: model.NewID(),
				WalletID:      request.FromWalletID,
				TransferID:    transfer.TransferID,
				EntryType:     model.EntryTypeDebit,
				Amount:        request.Amount,
			},
			{
				LedgerEntryID: model.NewID(),
				WalletID:      request.ToWalletID,
				TransferID:    transfer.TransferID,
				EntryType:     model.EntryTypeCredit,
				Amount:        request.Amount,
			},
		}
		if err := s.ledgerRepo.CreateEntries(ctx, tx, entries); err != nil {
			return err
		}

		if err := s.transferRepo.UpdateTransferState(ctx, tx, transfer.TransferID, model.TransferStateProcessed); err != nil {
			return err
		}
		transfer.State = model.TransferStateProcessed

		resp := buildResponse(&transfer, newFrom, newTo)
		if err := s.persistIdempotency(ctx, tx, request.IdempotencyKey, transfer.TransferID, resp); err != nil {
			return err
		}
		response = resp
		return nil
	})

	// A concurrent duplicate committed first: roll back this attempt and return
	// the winner's stored result instead of an error.
	if errors.Is(err, errIdempotencyConflict) {
		return s.replayStoredResponse(ctx, request.IdempotencyKey)
	}
	if err != nil {
		s.log().Error("create transfer failed key=%s: %v", request.IdempotencyKey, err)
		return TransferResponse{}, err
	}
	if businessErr != nil {
		s.log().Info("transfer not processed key=%s state=%s", request.IdempotencyKey, response.State)
		return response, businessErr
	}
	s.log().Info("transfer processed key=%s transferId=%s amount=%d", request.IdempotencyKey, response.TransferID, response.Amount)
	return response, nil
}

// lockWalletPair acquires FOR UPDATE locks on both wallets in a deterministic
// (sorted) order to avoid deadlocks, then returns them mapped to from/to roles.
func (s *transferService) lockWalletPair(ctx context.Context, tx *gorm.DB, fromID, toID string) (from, to *model.Wallet, err error) {
	first, second := fromID, toID
	if first > second {
		first, second = second, first
	}

	w1, err := s.walletRepo.GetWalletForUpdate(ctx, tx, first)
	if err != nil {
		return nil, nil, walletLookupError(err)
	}
	w2, err := s.walletRepo.GetWalletForUpdate(ctx, tx, second)
	if err != nil {
		return nil, nil, walletLookupError(err)
	}

	byID := map[string]*model.Wallet{first: w1, second: w2}
	return byID[fromID], byID[toID], nil
}

// persistIdempotency stores the response snapshot under the idempotency key. A
// unique violation means a concurrent duplicate won the race; it is converted
// to errIdempotencyConflict so the caller can replay the stored result.
func (s *transferService) persistIdempotency(ctx context.Context, tx *gorm.DB, key, transferID string, resp TransferResponse) error {
	payload, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	record := model.IdempotencyRecord{
		IdempotencyKey: key,
		TransferID:     transferID,
		Response:       string(payload),
	}
	if err := s.idempotencyRepo.Create(ctx, tx, record); err != nil {
		if repository.IsUniqueViolation(err) {
			return errIdempotencyConflict
		}
		return err
	}
	return nil
}

// replayStoredResponse reads a previously committed idempotency result.
func (s *transferService) replayStoredResponse(ctx context.Context, key string) (TransferResponse, error) {
	var response TransferResponse
	var businessErr error
	err := s.withTx(ctx, func(tx *gorm.DB) error {
		existing, err := s.idempotencyRepo.GetByKey(ctx, tx, key)
		if err != nil {
			return err
		}
		stored, err := decodeResponse(existing.Response)
		if err != nil {
			return err
		}
		response = stored
		businessErr = businessErrorFor(stored.State)
		return nil
	})
	if err != nil {
		return TransferResponse{}, err
	}
	return response, businessErr
}

func (s *transferService) log() *logging.Logger {
	if s.logger == nil {
		return logging.NewLogger(logging.LevelError, nil)
	}
	return s.logger
}

// buildResponse assembles a transport response from a transfer and the balances
// captured at the moment of execution.
func buildResponse(transfer *model.Transfer, fromBalance, toBalance int64) TransferResponse {
	return TransferResponse{
		TransferID:        transfer.TransferID,
		FromWalletID:      transfer.FromWalletID,
		ToWalletID:        transfer.ToWalletID,
		Amount:            transfer.Amount,
		State:             transfer.State,
		FromWalletBalance: fromBalance,
		ToWalletBalance:   toBalance,
		CreatedAt:         transfer.CreatedAt,
	}
}

func decodeResponse(payload string) (TransferResponse, error) {
	var response TransferResponse
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		return TransferResponse{}, err
	}
	return response, nil
}

func businessErrorFor(state model.TransferState) error {
	if state == model.TransferStateFailed {
		return ErrInsufficientFunds
	}
	return nil
}

func walletLookupError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return ErrWalletNotFound
	}
	return err
}
