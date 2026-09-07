package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"wallet-transfer-service/internal/apperror"
	"wallet-transfer-service/internal/model"
	"wallet-transfer-service/internal/repository"
)

type WalletService interface {
	CreateWallet(ctx context.Context, req *model.CreateWalletRequest) (*model.Wallet, int, error)
	GetWallet(ctx context.Context, id string) (*model.Wallet, int, error)
	UpdateWalletStatus(ctx context.Context, id string, status model.WalletStatus) (int, error)
	GetStatement(ctx context.Context, id string) (*model.StatementResponse, int, error)
}

type walletService struct {
	walletRepo   repository.WalletRepository
	transferRepo repository.TransferRepository
}

func NewWalletService(
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
) WalletService {
	return &walletService{
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
	}
}

func (s *walletService) CreateWallet(ctx context.Context, req *model.CreateWalletRequest) (*model.Wallet, int, error) {
	if req.ID == "" {
		return nil, http.StatusBadRequest, apperror.ErrWalletIDRequired
	}

	initialBalanceMinor, err := model.ParseToMinorUnits(req.InitialBalance)
	if err != nil || initialBalanceMinor < 0 {
		slog.WarnContext(ctx, "failed wallet creation: invalid or negative initial balance",
			"initial_balance", req.InitialBalance,
		)
		return nil, http.StatusBadRequest, apperror.ErrInvalidInitialBalance
	}

	status := req.Status
	if status == "" {
		status = model.WalletStatusActive
	}

	wallet := &model.Wallet{
		ID:      req.ID,
		Balance: initialBalanceMinor,
		Status:  status,
	}

	slog.InfoContext(ctx, "creating new wallet",
		"wallet_id", req.ID,
		"initial_balance_minor", initialBalanceMinor,
		"status", status,
	)

	if err := s.walletRepo.Create(ctx, wallet); err != nil {
		if errors.Is(err, apperror.ErrWalletAlreadyExists) {
			slog.WarnContext(ctx, "wallet already exists", "wallet_id", req.ID)
			return nil, http.StatusConflict, err
		}
		slog.ErrorContext(ctx, "database error creating wallet",
			"wallet_id", req.ID,
			"error", err,
		)
		return nil, http.StatusInternalServerError, fmt.Errorf("could not create wallet: %w", err)
	}

	slog.InfoContext(ctx, "wallet created successfully", "wallet_id", req.ID)
	return wallet, http.StatusCreated, nil
}

func (s *walletService) GetWallet(ctx context.Context, id string) (*model.Wallet, int, error) {
	if id == "" {
		return nil, http.StatusBadRequest, apperror.ErrWalletIDRequired
	}

	wallet, err := s.walletRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, apperror.ErrWalletNotFound) {
			slog.WarnContext(ctx, "wallet lookup failed: not found", "wallet_id", id)
			return nil, http.StatusNotFound, err
		}
		slog.ErrorContext(ctx, "database error querying wallet",
			"wallet_id", id,
			"error", err,
		)
		return nil, http.StatusInternalServerError, fmt.Errorf("could not fetch wallet: %w", err)
	}

	slog.DebugContext(ctx, "wallet fetched successfully", "wallet_id", id)
	return wallet, http.StatusOK, nil
}

func (s *walletService) UpdateWalletStatus(ctx context.Context, id string, status model.WalletStatus) (int, error) {
	if id == "" {
		return http.StatusBadRequest, apperror.ErrWalletIDRequired
	}

	slog.InfoContext(ctx, "updating wallet status",
		"wallet_id", id,
		"new_status", status,
	)

	if err := s.walletRepo.UpdateStatus(ctx, id, status); err != nil {
		if errors.Is(err, apperror.ErrWalletNotFound) {
			slog.WarnContext(ctx, "wallet status update failed: not found", "wallet_id", id)
			return http.StatusNotFound, err
		}
		slog.ErrorContext(ctx, "database error updating wallet status",
			"wallet_id", id,
			"error", err,
		)
		return http.StatusInternalServerError, fmt.Errorf("could not update wallet status: %w", err)
	}

	slog.InfoContext(ctx, "wallet status updated successfully",
		"wallet_id", id,
		"status", status,
	)
	return http.StatusOK, nil
}

func (s *walletService) GetStatement(ctx context.Context, id string) (*model.StatementResponse, int, error) {
	if id == "" {
		return nil, http.StatusBadRequest, apperror.ErrWalletIDRequired
	}

	wallet, err := s.walletRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, apperror.ErrWalletNotFound) {
			slog.WarnContext(ctx, "statement lookup failed: wallet not found", "wallet_id", id)
			return nil, http.StatusNotFound, err
		}
		slog.ErrorContext(ctx, "database error fetching wallet for statement", "wallet_id", id, "error", err)
		return nil, http.StatusInternalServerError, fmt.Errorf("failed fetching wallet: %w", err)
	}

	records, err := s.transferRepo.GetStatementByWalletID(ctx, id)
	if err != nil {
		slog.ErrorContext(ctx, "database error fetching wallet statement", "wallet_id", id, "error", err)
		return nil, http.StatusInternalServerError, fmt.Errorf("failed fetching statement records: %w", err)
	}

	entries := make([]model.StatementEntry, 0, len(records))
	for _, r := range records {
		entries = append(entries, model.StatementEntry{
			TransferID:      r.TransferID,
			Type:            r.Type,
			CounterpartID:   r.CounterpartID,
			Amount:          model.FormatMinorUnits(r.Amount),
			BalanceBefore:   model.FormatMinorUnits(r.BalanceBefore),
			BalanceAfter:    model.FormatMinorUnits(r.BalanceAfter),
			Status:          r.Status,
			TransactionDate: r.CreatedAt,
		})
	}

	statement := &model.StatementResponse{
		WalletID:     wallet.ID,
		Balance:      model.FormatMinorUnits(wallet.Balance),
		Transactions: entries,
	}

	slog.InfoContext(ctx, "wallet statement built successfully", "wallet_id", id, "transaction_count", len(entries))
	return statement, http.StatusOK, nil
}
