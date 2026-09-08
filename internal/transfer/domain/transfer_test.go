package domain

import "testing"

func TestCreateRequestValidate(t *testing.T) {
	valid := CreateRequest{IdempotencyKey: "key", FromWalletID: "a", ToWalletID: "b", Amount: 10}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	cases := []CreateRequest{
		{},
		{IdempotencyKey: "key", FromWalletID: "a", ToWalletID: "b", Amount: 0},
		{IdempotencyKey: "key", FromWalletID: "a", ToWalletID: "a", Amount: 10},
	}
	for _, request := range cases {
		if err := request.Validate(); err != ErrInvalidRequest {
			t.Fatalf("expected invalid request, got %v", err)
		}
	}
}
