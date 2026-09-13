package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"wallet-transfer-assigment/models"

	"go.uber.org/zap"
)

type walletHandler interface {
	getWallets(w http.ResponseWriter, r *http.Request)
	getWalletById(w http.ResponseWriter, r *http.Request)
}

func (s *Server) getWalletById(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	response := models.Response{}
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
		s.logger.Error("failed to fetch wallets", zap.Error(err))
		response = models.Response{
			Message: "failed to fetch wallets",
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
	response := models.Response{}
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
		s.logger.Error("failed to fetch wallets", zap.Error(err))
		response = models.Response{
			Message: "failed to fetch wallets",
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
