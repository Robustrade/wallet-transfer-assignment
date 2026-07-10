package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/dto"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/service"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/construct"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/utils"
)

const (
	transferTimeout = 5 * time.Second
	balanceTimeout  = 5 * time.Second
	historyTimeout  = 5 * time.Second
)

func TransferHandler(walletSvc service.IWalletSvc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), transferTimeout)
		defer cancel()

		var req dto.TransferRequest

		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			logger.Warn().Err(err).Msg("error decoding request body")
			utils.WriteError(w, construct.ErrInvalidRequestBody)
			return
		}

		if req.IdempotencyKey == "" || req.FromWalletID == "" || req.ToWalletID == "" || req.Amount <= 0 {
			logger.Warn().Interface("request", req).Msg("missing or invalid transfer data")
			utils.WriteError(w, construct.ErrMissingTransferData)
			return
		}

		if req.FromWalletID == req.ToWalletID {
			logger.Warn().Str("wallet_id", req.FromWalletID).Msg("transfer request has same source and destination wallet")
			utils.WriteError(w, construct.ErrSameWallet)
			return
		}

		logger.Debug().
			Str("idempotency_key", req.IdempotencyKey).
			Str("from_wallet_id", req.FromWalletID).
			Str("to_wallet_id", req.ToWalletID).
			Float64("amount", req.Amount).
			Msg("received transfer request")

		transactionID, status, timestamp, err := walletSvc.TransferFunds(ctx, req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount)
		if err != nil {
			logger.Warn().Err(err).Str("idempotency_key", req.IdempotencyKey).Msg("transfer request failed")
			utils.WriteError(w, construct.MapTransferError(err))
			return
		}

		resp := dto.TransferResponse{
			TransactionID: transactionID,
			FromWalletID:  req.FromWalletID,
			ToWalletID:    req.ToWalletID,
			Amount:        req.Amount,
			Status:        status,
			Timestamp:     timestamp,
		}

		utils.WriteSuccess(w, http.StatusOK, resp)
	})
}

func WalletBalanceHandler(walletSvc service.IWalletSvc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), balanceTimeout)
		defer cancel()

		walletID := mux.Vars(r)["id"]
		if walletID == "" {
			logger.Warn().Msg("missing wallet id in balance request")
			utils.WriteError(w, construct.ErrMissingWalletID)
			return
		}

		logger.Debug().Str("wallet_id", walletID).Msg("received wallet balance request")

		ownerName, balance, err := walletSvc.GetWalletBalance(ctx, walletID)
		if err != nil {
			logger.Warn().Err(err).Str("wallet_id", walletID).Msg("wallet balance request failed")
			utils.WriteError(w, construct.MapTransferError(err))
			return
		}

		resp := dto.WalletBalanceResponse{
			WalletID:  walletID,
			OwnerName: ownerName,
			Balance:   balance,
		}

		utils.WriteSuccess(w, http.StatusOK, resp)
	})
}

func TransferHistoryHandler(walletSvc service.IWalletSvc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), historyTimeout)
		defer cancel()

		walletID := mux.Vars(r)["id"]
		if walletID == "" {
			logger.Warn().Msg("missing wallet id in transfer history request")
			utils.WriteError(w, construct.ErrMissingWalletID)
			return
		}

		limit, err := parseQueryInt(r, "limit", 0)
		if err != nil {
			logger.Warn().Err(err).Str("wallet_id", walletID).Msg("invalid limit query param")
			utils.WriteError(w, construct.ErrInvalidPagination)
			return
		}

		offset, err := parseQueryInt(r, "offset", 0)
		if err != nil {
			logger.Warn().Err(err).Str("wallet_id", walletID).Msg("invalid offset query param")
			utils.WriteError(w, construct.ErrInvalidPagination)
			return
		}

		logger.Debug().Str("wallet_id", walletID).Int32("limit", limit).Int32("offset", offset).Msg("received transfer history request")

		transfers, limit, offset, err := walletSvc.GetTransferHistory(ctx, walletID, limit, offset)
		if err != nil {
			logger.Warn().Err(err).Str("wallet_id", walletID).Msg("transfer history request failed")
			utils.WriteError(w, construct.MapTransferError(err))
			return
		}

		entries := make([]dto.TransferHistoryEntry, len(transfers))
		for i, t := range transfers {
			entryType := domain.LedgerCredit
			if t.FromWalletID == walletID {
				entryType = domain.LedgerDebit
			}
			entries[i] = dto.TransferHistoryEntry{
				TransactionID: t.ID,
				Type:          string(entryType),
				FromWalletID:  t.FromWalletID,
				ToWalletID:    t.ToWalletID,
				Amount:        t.Amount,
				Status:        string(t.Status),
				Timestamp:     t.CreatedAt.UnixMilli(),
			}
		}

		resp := dto.TransferHistoryResponse{
			WalletID:  walletID,
			Transfers: entries,
			Limit:     int(limit),
			Offset:    int(offset),
		}

		utils.WriteSuccess(w, http.StatusOK, resp)
	})
}

func parseQueryInt(r *http.Request, key string, defaultVal int32) (int32, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return defaultVal, nil
	}
	val, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(val), nil
}
