package wallet

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPWalletAndTransferContract(t *testing.T) {
	service, _ := newTestService(t)
	server := httptest.NewServer(NewHTTPHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	if response := sendJSON(t, server.Client(), http.MethodGet, server.URL+"/health", ""); response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", response.Code)
	}
	createWalletResponse := sendJSON(t, server.Client(), http.MethodPost, server.URL+"/wallets",
		`{"walletId":"http-source","initialBalance":50}`)
	if createWalletResponse.Code != http.StatusCreated {
		t.Fatalf("create source status = %d, body=%s", createWalletResponse.Code, createWalletResponse.Body)
	}
	sendJSON(t, server.Client(), http.MethodPost, server.URL+"/wallets",
		`{"walletId":"http-destination"}`)

	request := `{"idempotencyKey":"http-key","fromWalletId":"http-source","toWalletId":"http-destination","amount":12.5}`
	first := sendJSON(t, server.Client(), http.MethodPost, server.URL+"/transfers", request)
	if first.Code != http.StatusCreated {
		t.Fatalf("transfer status = %d, body=%s", first.Code, first.Body)
	}
	var response TransferResult
	if err := json.Unmarshal(first.Body, &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != "PROCESSED" || response.Amount != 1250 {
		t.Fatalf("unexpected transfer response: %+v", response)
	}
	balance := sendJSON(t, server.Client(), http.MethodGet, server.URL+"/wallets/http-source", "")
	var walletResponse Wallet
	if err := json.Unmarshal(balance.Body, &walletResponse); err != nil {
		t.Fatal(err)
	}
	if walletResponse.Balance != 3750 {
		t.Fatalf("source balance = %d, want 3750", walletResponse.Balance)
	}

	replay := sendJSON(t, server.Client(), http.MethodPost, server.URL+"/transfers", request)
	if replay.Code != http.StatusCreated {
		t.Fatalf("replay status = %d, want 201", replay.Code)
	}
	var replayResponse TransferResult
	if err := json.Unmarshal(replay.Body, &replayResponse); err != nil {
		t.Fatal(err)
	}
	if replayResponse.TransferID != response.TransferID {
		t.Fatalf("replay transfer ID = %q, want %q", replayResponse.TransferID, response.TransferID)
	}
}

func TestHTTPErrorResponses(t *testing.T) {
	service, _ := newTestService(t)
	server := httptest.NewServer(NewHTTPHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	for _, test := range []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
	}{
		{name: "non-object body", body: `[]`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "trailing JSON", body: `{} {}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "invalid amount", body: `{"idempotencyKey":"x","fromWalletId":"a","toWalletId":"b","amount":1.001}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "wrong content type", body: `{}`, contentType: "text/plain", wantStatus: http.StatusBadRequest},
		{name: "unknown wallet", body: `{"idempotencyKey":"x","fromWalletId":"missing","toWalletId":"also-missing","amount":1}`, contentType: "application/json", wantStatus: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(
				http.MethodPost,
				server.URL+"/transfers",
				strings.NewReader(test.body),
			)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", test.contentType)
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.wantStatus {
				data, _ := io.ReadAll(response.Body)
				t.Fatalf("status = %d, body=%s; want %d", response.StatusCode, data, test.wantStatus)
			}
		})
	}
}

func TestFailedTransferReturns422(t *testing.T) {
	service, _ := newTestService(t)
	_, err := service.CreateWallet(context.Background(), CreateWalletRequest{
		WalletID:       stringPointer("small"),
		InitialBalance: json.RawMessage("1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateWallet(context.Background(), CreateWalletRequest{
		WalletID: stringPointer("empty"),
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHTTPHandler(service, nil))
	defer server.Close()

	response := sendJSON(t, server.Client(), http.MethodPost, server.URL+"/transfers",
		`{"idempotencyKey":"insufficient","fromWalletId":"small","toWalletId":"empty","amount":2}`)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.Code)
	}
}

type capturedResponse struct {
	Code int
	Body []byte
}

func sendJSON(
	t *testing.T,
	client *http.Client,
	method, url, body string,
) capturedResponse {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return capturedResponse{Code: response.StatusCode, Body: data}
}
