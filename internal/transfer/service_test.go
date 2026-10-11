package transfer

import (
	"context"
	"errors"
	"testing"
)

type repositoryFunc func(context.Context, CreateRequest, [32]byte) (Outcome, error)

func (f repositoryFunc) Create(ctx context.Context, request CreateRequest, hash [32]byte) (Outcome, error) {
	return f(ctx, request, hash)
}

func TestCreateRejectsInvalidRequestsBeforePersistence(t *testing.T) {
	cases := []struct {
		name    string
		request CreateRequest
	}{
		{name: "missing idempotency key", request: CreateRequest{FromWalletID: "a", ToWalletID: "b", Amount: 1}},
		{name: "missing source", request: CreateRequest{IdempotencyKey: "key", ToWalletID: "b", Amount: 1}},
		{name: "missing destination", request: CreateRequest{IdempotencyKey: "key", FromWalletID: "a", Amount: 1}},
		{name: "same wallet", request: CreateRequest{IdempotencyKey: "key", FromWalletID: "a", ToWalletID: "a", Amount: 1}},
		{name: "zero amount", request: CreateRequest{IdempotencyKey: "key", FromWalletID: "a", ToWalletID: "b", Amount: 0}},
		{name: "negative amount", request: CreateRequest{IdempotencyKey: "key", FromWalletID: "a", ToWalletID: "b", Amount: -1}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			called := false
			service := NewService(repositoryFunc(func(context.Context, CreateRequest, [32]byte) (Outcome, error) {
				called = true
				return Outcome{}, nil
			}))

			_, err := service.Create(context.Background(), testCase.request)
			var apiError *Error
			if !errors.As(err, &apiError) || apiError.StatusCode != 400 {
				t.Fatalf("expected invalid request error, got %v", err)
			}
			if called {
				t.Fatal("repository was called for an invalid request")
			}
		})
	}
}

func TestCreatePassesStableRequestHashToRepository(t *testing.T) {
	var hashes [][32]byte
	service := NewService(repositoryFunc(func(_ context.Context, _ CreateRequest, hash [32]byte) (Outcome, error) {
		hashes = append(hashes, hash)
		return Outcome{StatusCode: 201, Body: []byte(`{"state":"PROCESSED"}`)}, nil
	}))

	request := CreateRequest{IdempotencyKey: "key", FromWalletID: "wallet-a", ToWalletID: "wallet-b", Amount: 125}
	if _, err := service.Create(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 2 {
		t.Fatalf("repository called %d times, want 2", len(hashes))
	}
	if hashes[0] != hashes[1] {
		t.Fatal("same request produced different hashes")
	}
	if hashes[0] == ([32]byte{}) {
		t.Fatal("request hash was empty")
	}
}
