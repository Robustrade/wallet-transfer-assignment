package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"wallet-transfer-assigment/models"
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
)

type walletHandler interface {
	getWallets(w http.ResponseWriter, r *http.Request)
	getWalletById(w http.ResponseWriter, r *http.Request)
	walletStatementByWalletId(w http.ResponseWriter, r *http.Request)
	createWallet(w http.ResponseWriter, r *http.Request)
}

func (s *Server) wallets(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.getWallets(w, r)
		return
	}
	if r.Method == http.MethodPost {
		s.createWallet(w, r)
		return
	}
	s.getWallets(w, r)
}

func (s *Server) createWallet(w http.ResponseWriter, r *http.Request) {
	var response models.Response
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		response = models.Response{
			Message: "Invalid method type",
			Data:    nil,
			Status:  fmt.Sprint(http.StatusMethodNotAllowed),
		}
		err := json.NewEncoder(w).Encode(response)
		if err != nil {
			s.logger.Error("unable to encode the response", zap.Error(err))
		}
		return
	}
	ctx := r.Context()
	request := models.WalletRequest{}
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		s.logger.Error("unable to decode request body ", zap.Error(err))
		response := models.Response{
			Message: "invalid request body",
			Data:    nil,
			Status:  fmt.Sprint(http.StatusBadRequest),
		}

		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	if err := utils.ValidateRequest(request); err != nil {
		s.logger.Error(
			"request validation failed",
			zap.Error(err),
		)

		w.WriteHeader(http.StatusBadRequest)

		response := models.Response{
			Message: "invalid request",
			Data:    nil,
			Status:  fmt.Sprint(http.StatusBadRequest),
		}

		_ = json.NewEncoder(w).Encode(response)
		return
	}
	walletId, err := s.Conrollerer.CreateWallet(ctx, request)
	if err != nil {
		s.logger.Error("unable to create wallet", zap.Error(err))
		response = models.Response{
			Message: "unsuccessful",
			Status:  fmt.Sprint(http.StatusInternalServerError),
		}
		w.WriteHeader(http.StatusInternalServerError)
		err = json.NewEncoder(w).Encode(response)
		if err != nil {
			s.logger.Error("unable to encode the response", zap.Error(err))
		}
		return
	}
	response = models.Response{
		Message: "successful",
		Data:    walletId,
		Status:  fmt.Sprint(http.StatusCreated),
	}
	w.WriteHeader(http.StatusCreated)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		s.logger.Error("unable to encode the response", zap.Error(err))
		return
	}

}
func (s *Server) walletStatementByWalletId(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var response models.Response
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		response = models.Response{
			Message: "Invalid method type",
			Data:    nil,
			Status:  fmt.Sprint(http.StatusMethodNotAllowed),
		}
		err := json.NewEncoder(w).Encode(response)
		if err != nil {
			s.logger.Error("unable to encode the response", zap.Error(err))
		}
		return
	}
	wallet_id := r.PathValue("id")
	if wallet_id == "" {
		response = models.Response{
			Message: "wallet id required",
			Status:  fmt.Sprint(http.StatusBadRequest),
		}
		w.WriteHeader(http.StatusBadRequest)
		err := json.NewEncoder(w).Encode(response)
		if err != nil {
			s.logger.Error("unable to encode the response", zap.Error(err))
		}
		return
	}
	walletStatements, err := s.Conrollerer.GetWalletStatementByWalletId(ctx, wallet_id)
	if err != nil {
		s.logger.Error("failed to fetch wallet statements", zap.Error(err))
		response = models.Response{
			Message: "failed to fetch wallet statements",
			Data:    nil,
			Status:  fmt.Sprint(http.StatusInternalServerError),
		}
		w.WriteHeader(http.StatusInternalServerError)
		err = json.NewEncoder(w).Encode(response)
		if err != nil {
			s.logger.Error("unable to encode the response", zap.Error(err))
		}
		return
	}
	response = models.Response{
		Data:    walletStatements,
		Message: "SUCCESS",
		Status:  fmt.Sprint(http.StatusOK),
	}
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		s.logger.Error("unable to encode the response", zap.Error(err))
	}
}

func (s *Server) getWalletById(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var response models.Response
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		response = models.Response{
			Message: "Invalid method type",
			Data:    nil,
			Status:  fmt.Sprint(http.StatusMethodNotAllowed),
		}
		err := json.NewEncoder(w).Encode(response)
		if err != nil {
			s.logger.Error("unable to encode the response", zap.Error(err))
		}
		return
	}
	wallet_id := r.PathValue("id")
	if wallet_id == "" {
		response = models.Response{
			Message: "wallet id required",
			Status:  fmt.Sprint(http.StatusBadRequest),
		}
		w.WriteHeader(http.StatusBadRequest)
		err := json.NewEncoder(w).Encode(response)
		if err != nil {
			s.logger.Error("unable to encode the response", zap.Error(err))
		}
		return
	}
	walletDetail, err := s.Conrollerer.GetWalletById(ctx, wallet_id)
	if err != nil {
		s.logger.Error("failed to fetch wallet statements", zap.Error(err))
		response = models.Response{
			Message: "failed to fetch wallet statements",
			Data:    nil,
			Status:  fmt.Sprint(http.StatusInternalServerError),
		}
		w.WriteHeader(http.StatusInternalServerError)
		err = json.NewEncoder(w).Encode(response)
		if err != nil {
			s.logger.Error("unable to encode the response", zap.Error(err))
		}
		return
	}
	response = models.Response{
		Data:    walletDetail,
		Message: "SUCCESS",
		Status:  fmt.Sprint(http.StatusOK),
	}
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		s.logger.Error("unable to encode the response", zap.Error(err))
	}

}
func (s *Server) getWallets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var response models.Response
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		response = models.Response{
			Message: "Invalid method type",
			Data:    nil,
			Status:  fmt.Sprint(http.StatusMethodNotAllowed),
		}
		err := json.NewEncoder(w).Encode(response)
		if err != nil {
			s.logger.Error("unable to encode the response", zap.Error(err))
		}
		return
	}
	walletDetails, err := s.Conrollerer.GetWalletsDetails(ctx)
	if err != nil {
		s.logger.Error("failed to fetch wallet statements", zap.Error(err))
		response = models.Response{
			Message: "failed to fetch wallet statements",
			Data:    nil,
			Status:  fmt.Sprint(http.StatusInternalServerError),
		}
		w.WriteHeader(http.StatusInternalServerError)
		err = json.NewEncoder(w).Encode(response)
		if err != nil {
			s.logger.Error("unable to encode the response", zap.Error(err))
		}
		return
	}
	response = models.Response{
		Data:    walletDetails,
		Message: "SUCCESS",
		Status:  fmt.Sprint(http.StatusOK),
	}
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		s.logger.Error("unable to encode the response", zap.Error(err))
	}

}
