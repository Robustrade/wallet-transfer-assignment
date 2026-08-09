package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/model"
	mockservice "github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/services/mock"
	"github.com/golang/mock/gomock"
)

func TestCreateTransfer_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	svc := mockservice.NewMockService(ctrl)
	controller := NewTransferController(svc)

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString("{invalid-json"))
	res := httptest.NewRecorder()

	controller.CreateTransfer(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}

	if !strings.Contains(res.Body.String(), "Invalid input") {
		t.Fatalf("expected invalid input response, got %q", res.Body.String())
	}
}

func TestCreateTransfer_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	svc := mockservice.NewMockService(ctrl)
	controller := NewTransferController(svc)

	input := model.CreateTransferRequest{
		IdempotencyKey: "key-123",
		FromWalletID:   "wallet-a",
		ToWalletID:     "wallet-b",
		Amount:         100,
		Status:         "PENDING",
	}

	expectedTransfer := model.Transfer{
		ID:             "transfer-1",
		IdempotencyKey: input.IdempotencyKey,
		FromWallet:     input.FromWalletID,
		ToWallet:       input.ToWalletID,
		Amount:         input.Amount,
		Status:         "PROCESSED",
	}

	svc.EXPECT().CreateTransfer(gomock.Any(), input).Return(expectedTransfer, nil)

	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(payload))
	res := httptest.NewRecorder()

	controller.CreateTransfer(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, res.Code)
	}

	var actual model.Transfer
	if err := json.NewDecoder(res.Body).Decode(&actual); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if actual != expectedTransfer {
		t.Fatalf("expected body %#v, got %#v", expectedTransfer, actual)
	}
}
