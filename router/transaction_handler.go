package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"wallet-transfer-assigment/models"
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
)

type transactionHandler interface {
	transfer(w http.ResponseWriter, r *http.Request)
	// transactionHandle(w http.ResponseWriter, r *http.Request)
	// transactionStatusHandle(w http.ResponseWriter, r *http.Request)
}

func (s *Server) transfer(w http.ResponseWriter, r *http.Request) {
	response := models.Response{}
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

	request := models.TransferRequest{}
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
	transaction_id, err := s.Conrollerer.Transfer(ctx, request)
	if err != nil {
		s.logger.Error("transaction failed", zap.Error(err))
		response = models.Response{
			Message: "transaction failed",
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
		Message: "Successfull",
		Data:    transaction_id,
		Status:  fmt.Sprint(http.StatusCreated),
	}
	w.WriteHeader(http.StatusCreated)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		s.logger.Error("unable to encode the response", zap.Error(err))
	}

}

// func (s *Server) transactionStatusHandle(w http.ResponseWriter, r *http.Request) {

// }
