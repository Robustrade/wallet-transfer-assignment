package model_test

import (
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/model"
)

func TestHashTransferRequest_Canonical(t *testing.T) {
	t.Parallel()
	hash, body, err := model.HashTransferRequest("wallet_1", "wallet_2", 100)
	if err != nil {
		t.Fatal(err)
	}
	wantBody := `{"amount":100,"fromWalletId":"wallet_1","toWalletId":"wallet_2"}`
	if string(body) != wantBody {
		t.Fatalf("body=%s want=%s", body, wantBody)
	}
	if len(hash) != 64 {
		t.Fatalf("hash len=%d", len(hash))
	}
	hash2, _, err := model.HashTransferRequest("wallet_1", "wallet_2", 100)
	if err != nil {
		t.Fatal(err)
	}
	if hash != hash2 {
		t.Fatal("hash must be stable")
	}
	hash3, _, _ := model.HashTransferRequest("wallet_1", "wallet_2", 101)
	if hash == hash3 {
		t.Fatal("different amount must change hash")
	}
}

func TestIdempotencyRecord_StoreResponse(t *testing.T) {
	t.Parallel()
	hash, body, _ := model.HashTransferRequest("a", "b", 1)
	rec, err := model.NewIdempotencyRecord("key-1", hash, body)
	if err != nil {
		t.Fatal(err)
	}
	if rec.HasResponse() {
		t.Fatal("should not have response yet")
	}
	if err := rec.StoreResponse(201, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if !rec.HasResponse() || *rec.ResponseCode != 201 {
		t.Fatalf("%+v", rec)
	}
	if err := rec.StoreResponse(422, []byte(`{}`)); err != model.ErrInvalidTransition {
		t.Fatalf("rewrite: %v", err)
	}
}
