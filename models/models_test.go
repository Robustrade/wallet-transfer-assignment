package models

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"wallet-transfer-assigment/utils"

	"github.com/go-playground/validator/v10"
)

func TestModelJSONRoundTrips(t *testing.T) {
	createdAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)

	tests := []struct {
		name  string
		value interface{}
		check func(t *testing.T, encoded []byte)
	}{
		{
			name: "wallet",
			value: Wallet{
				WalletID:  "wallet_1",
				Balance:   1250,
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
		},
		{
			name: "transfer",
			value: Transfers{
				TransactionID: "transaction_1",
				FromWalletID:  "wallet_1",
				ToWalletID:    "wallet_2",
				Amount:        250,
				Status:        utils.PROCESSED,
				Comments:      "wallet transfer",
				CreatedAt:     createdAt,
				UpdatedAt:     updatedAt,
			},
		},
		{
			name: "ledger entry",
			value: LedgerEntries{
				WalletID:      "wallet_1",
				Amount:        250,
				Type:          utils.Debit,
				TransactionID: "transaction_1",
				CreatedAt:     createdAt,
				UpdatedAt:     updatedAt,
			},
		},
		{
			name: "idempotency record",
			value: IdempotencyRecords{
				IdempotencyKey: "request_1",
				TransactionID:  "transaction_1",
				CreatedAt:      createdAt,
				UpdatedAt:      updatedAt,
			},
		},
		{
			name: "response",
			value: Response{
				Message: "transfer processed",
				Data:    map[string]interface{}{"transaction_id": "transaction_1"},
				Status:  "success",
			},
		},
		{
			name: "transfer request",
			value: TransferRequest{
				IdempotencyKey: "request_1",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         250,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatalf("marshal model: %v", err)
			}

			decoded := reflect.New(reflect.TypeOf(test.value))
			if err := json.Unmarshal(encoded, decoded.Interface()); err != nil {
				t.Fatalf("unmarshal model: %v", err)
			}

			if !reflect.DeepEqual(test.value, decoded.Elem().Interface()) {
				t.Errorf("round trip mismatch\nwant: %#v\n got: %#v", test.value, decoded.Elem().Interface())
			}
		})
	}
}

func TestTransferRequestValidation(t *testing.T) {
	validRequest := TransferRequest{
		IdempotencyKey: "request_1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	tests := []struct {
		name  string
		value TransferRequest
		valid bool
	}{
		{name: "valid request", value: validRequest, valid: true},
		{name: "missing idempotency key", value: TransferRequest{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}},
		{name: "missing source wallet", value: TransferRequest{IdempotencyKey: "request_1", ToWalletID: "wallet_2", Amount: 100}},
		{name: "missing destination wallet", value: TransferRequest{IdempotencyKey: "request_1", FromWalletID: "wallet_1", Amount: 100}},
		{name: "zero amount", value: TransferRequest{IdempotencyKey: "request_1", FromWalletID: "wallet_1", ToWalletID: "wallet_2"}},
		{name: "negative amount", value: TransferRequest{IdempotencyKey: "request_1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: -1}},
	}

	validate := validator.New()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validate.Struct(test.value)
			if test.valid && err != nil {
				t.Fatalf("valid request failed validation: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("invalid request passed validation")
			}
		})
	}
}
