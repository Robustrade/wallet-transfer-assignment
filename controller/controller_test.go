package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	datasource "wallet-transfer-assigment/datasource"
	db "wallet-transfer-assigment/internal/db/generated"
	"wallet-transfer-assigment/mapper"
	"wallet-transfer-assigment/models"

	"go.uber.org/zap"
)

type controllerTestLogger struct{}

func (controllerTestLogger) Info(string, ...zap.Field)  {}
func (controllerTestLogger) Error(string, ...zap.Field) {}
func (controllerTestLogger) Warn(string, ...zap.Field)  {}
func (controllerTestLogger) Debug(string, ...zap.Field) {}
func (controllerTestLogger) Sync()                      {}

type controllerDataSourceMock struct {
	wallets       []db.Wallet
	wallet        db.Wallet
	transferID    string
	walletsErr    error
	walletErr     error
	transferErr   error
	transferParam db.CreateTransferParams
	idempotency   string
}

func (mock *controllerDataSourceMock) GetWallets(context.Context) ([]db.Wallet, error) {
	return mock.wallets, mock.walletsErr
}

func (mock *controllerDataSourceMock) GetWalletById(context.Context, string) (db.Wallet, error) {
	return mock.wallet, mock.walletErr
}

func (mock *controllerDataSourceMock) Transfer(_ context.Context, request db.CreateTransferParams, idempotencyKey string) (*string, error) {
	mock.transferParam = request
	mock.idempotency = idempotencyKey
	if mock.transferErr != nil {
		return nil, mock.transferErr
	}
	return &mock.transferID, nil
}

type controllerMapperMock struct {
	walletResponse  models.Wallet
	walletsResponse []models.Wallet
	transferParams  db.CreateTransferParams
	walletCalls     int
	walletsCalls    int
	transferCalls   int
}

func (mock *controllerMapperMock) ToWalletResponse(db.Wallet) models.Wallet {
	mock.walletCalls++
	return mock.walletResponse
}

func (mock *controllerMapperMock) ToWalletsResponse([]db.Wallet) []models.Wallet {
	mock.walletsCalls++
	return mock.walletsResponse
}

func (mock *controllerMapperMock) ToCreateTransferParams(models.TransferRequest) db.CreateTransferParams {
	mock.transferCalls++
	return mock.transferParams
}

func newControllerTest(dataSource *controllerDataSourceMock, mapperMock *controllerMapperMock) Controller {
	return Controller{
		db:    dataSource,
		log:   controllerTestLogger{},
		maper: mapperMock,
	}
}

func TestControllerTransferDelegatesAndReturnsTransactionID(t *testing.T) {
	dataSource := &controllerDataSourceMock{transferID: "transaction_1"}
	mapperMock := &controllerMapperMock{transferParams: db.CreateTransferParams{
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_2",
		Amount:       100,
		Status:       "PENDING",
	}}
	controller := newControllerTest(dataSource, mapperMock)
	request := models.TransferRequest{IdempotencyKey: "request_1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}

	got, err := controller.Transfer(context.Background(), request)
	if err != nil {
		t.Fatalf("transfer returned error: %v", err)
	}
	if got == nil || *got != "transaction_1" {
		t.Fatalf("transaction ID: got %v, want transaction_1", got)
	}
	if mapperMock.transferCalls != 1 {
		t.Fatalf("transfer mapper calls: got %d, want 1", mapperMock.transferCalls)
	}
	if dataSource.transferParam != mapperMock.transferParams || dataSource.idempotency != request.IdempotencyKey {
		t.Fatalf("transfer delegation mismatch: params=%#v key=%q", dataSource.transferParam, dataSource.idempotency)
	}
}

func TestControllerTransferReturnsDatasourceError(t *testing.T) {
	wantErr := errors.New("transfer failed")
	dataSource := &controllerDataSourceMock{transferErr: wantErr}
	mapperMock := &controllerMapperMock{}
	controller := newControllerTest(dataSource, mapperMock)

	_, err := controller.Transfer(context.Background(), models.TransferRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error: got %v, want %v", err, wantErr)
	}
}

func TestControllerGetWalletsDetailsMapsDatasourceWallets(t *testing.T) {
	dataSource := &controllerDataSourceMock{wallets: []db.Wallet{{WalletID: "wallet_1", Balance: 100}}}
	mapperMock := &controllerMapperMock{walletsResponse: []models.Wallet{{WalletID: "wallet_1", Balance: 100}}}
	controller := newControllerTest(dataSource, mapperMock)

	got, err := controller.GetWalletsDetails(context.Background())
	if err != nil {
		t.Fatalf("get wallets returned error: %v", err)
	}
	if len(got) != 1 || got[0] != mapperMock.walletsResponse[0] {
		t.Fatalf("wallet response: got %#v, want %#v", got, mapperMock.walletsResponse)
	}
	if mapperMock.walletsCalls != 1 {
		t.Fatalf("wallet list mapper calls: got %d, want 1", mapperMock.walletsCalls)
	}
}

func TestControllerGetWalletsDetailsReturnsDatasourceError(t *testing.T) {
	wantErr := errors.New("wallet list failed")
	dataSource := &controllerDataSourceMock{walletsErr: wantErr}
	mapperMock := &controllerMapperMock{}
	controller := newControllerTest(dataSource, mapperMock)

	_, err := controller.GetWalletsDetails(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("error: got %v, want %v", err, wantErr)
	}
	if mapperMock.walletsCalls != 0 {
		t.Fatal("wallet mapper should not be called after datasource failure")
	}
}

func TestControllerGetWalletByIDMapsDatasourceWallet(t *testing.T) {
	createdAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	dataSource := &controllerDataSourceMock{wallet: db.Wallet{WalletID: "wallet_1", Balance: 100, CreatedAt: createdAt}}
	mapperMock := &controllerMapperMock{walletResponse: models.Wallet{WalletID: "wallet_1", Balance: 100, CreatedAt: createdAt}}
	controller := newControllerTest(dataSource, mapperMock)

	got, err := controller.GetWalletById(context.Background(), "wallet_1")
	if err != nil {
		t.Fatalf("get wallet returned error: %v", err)
	}
	if got == nil || *got != mapperMock.walletResponse {
		t.Fatalf("wallet response: got %#v, want %#v", got, mapperMock.walletResponse)
	}
	if mapperMock.walletCalls != 1 {
		t.Fatalf("wallet mapper calls: got %d, want 1", mapperMock.walletCalls)
	}
}

func TestControllerGetWalletByIDReturnsDatasourceError(t *testing.T) {
	wantErr := errors.New("wallet lookup failed")
	dataSource := &controllerDataSourceMock{walletErr: wantErr}
	mapperMock := &controllerMapperMock{}
	controller := newControllerTest(dataSource, mapperMock)

	_, err := controller.GetWalletById(context.Background(), "wallet_1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("error: got %v, want %v", err, wantErr)
	}
	if mapperMock.walletCalls != 0 {
		t.Fatal("wallet mapper should not be called after datasource failure")
	}
}

var _ datasource.DataSourceer = (*controllerDataSourceMock)(nil)
var _ mapper.Mapper = (*controllerMapperMock)(nil)
