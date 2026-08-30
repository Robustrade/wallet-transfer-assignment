package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codejk-crypto/wallet-transfer-assignment/internal/handler"
	"github.com/codejk-crypto/wallet-transfer-assignment/internal/service"
	"github.com/codejk-crypto/wallet-transfer-assignment/internal/testutil"
)

func TestHTTPCreateTransferAndReplay(t *testing.T) {
	svc := service.New(testutil.Pool(t), nil)
	srv := httptest.NewServer(handler.New(svc).Routes())
	t.Cleanup(srv.Close)

	createWallet(t, srv.URL, `{"id":"wallet_1","balance":100}`)
	createWallet(t, srv.URL, `{"id":"wallet_2","balance":0}`)

	body := `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`
	first := postJSON(t, srv.URL+"/transfers", body)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("status %d body %s", first.StatusCode, first.Body)
	}
	second := postJSON(t, srv.URL+"/transfers", body)
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("replay status %d body %s", second.StatusCode, second.Body)
	}
	var a, b map[string]any
	if err := json.Unmarshal([]byte(first.Body), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(second.Body), &b); err != nil {
		t.Fatal(err)
	}
	if a["id"] != b["id"] {
		t.Fatalf("replay changed id %v vs %v", a["id"], b["id"])
	}

	w1 := getJSON(t, srv.URL+"/wallets/wallet_1")
	if w1.StatusCode != http.StatusOK {
		t.Fatalf("get wallet %d %s", w1.StatusCode, w1.Body)
	}
}

func TestHTTPValidation(t *testing.T) {
	svc := service.New(testutil.Pool(t), nil)
	srv := httptest.NewServer(handler.New(svc).Routes())
	t.Cleanup(srv.Close)

	res := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"","fromWalletId":"a","toWalletId":"b","amount":1}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d body %s", res.StatusCode, res.Body)
	}

	trailing := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1}{"x":1}`)
	if trailing.StatusCode != http.StatusBadRequest {
		t.Fatalf("trailing json status %d body %s", trailing.StatusCode, trailing.Body)
	}
}

type httpResult struct {
	StatusCode int
	Body       string
}

func createWallet(t *testing.T, base, body string) {
	t.Helper()
	res := postJSON(t, base+"/wallets", body)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create wallet %d %s", res.StatusCode, res.Body)
	}
}

func postJSON(t *testing.T, url, body string) httpResult {
	t.Helper()
	res, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	return httpResult{StatusCode: res.StatusCode, Body: buf.String()}
}

func getJSON(t *testing.T, url string) httpResult {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	return httpResult{StatusCode: res.StatusCode, Body: buf.String()}
}
