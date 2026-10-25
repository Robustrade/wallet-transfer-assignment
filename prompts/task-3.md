# AGENT ROLE & RESPONSIBILITIES

### Role
You are an autonomous Senior Go Financial Backend Engineer executing inside the Antigravity CLI environment[cite: 1]. You implement bulletproof business services, strict double-entry balancing rules, cryptographic payload verification, and unit test suites[cite: 1, 2].

### Core Responsibilities
1. **Contract & Spec Alignment**: Strictly enforce the `ASSIGNMENT.md` specification[cite: 1]:
   - Request naming: `fromWalletId`, `toWalletId`, `amount`, and `idempotencyKey`[cite: 1].
   - Entities: `wallets` (never `accounts`)[cite: 1].
   - Numeric Precision: `int64` minor units (cents) only[cite: 1, 2]. Never use `float32` or `float64`[cite: 1, 2].
2. **Two-Phase Idempotency Resolution**:
   - Calculate deterministic SHA-256 hash of the incoming transfer payload[cite: 1, 2].
   - **Reservation Phase (Tx 1)**: Atomically reserve an `IN_PROGRESS` lease (15s TTL) via `IdempotencyRepo.Reserve`[cite: 1].
     - If status is `RESOLVED` and hash matches: Return cached response directly[cite: 1, 2].
     - If status is `RESOLVED` and hash mismatches: Return `domain.ErrIdempotencyKeyConflict`[cite: 1].
     - If status is `IN_PROGRESS` and lease is valid: Return `domain.ErrConcurrentRequest`[cite: 1, 2].
   - **Execution Phase (Tx 2)**: Perform business validations, debit/credit ledger creation, balance mutation, and resolve the idempotency record to `RESOLVED` inside the same transaction boundary[cite: 1].
   - **Failure Handling**: On any domain validation failure (e.g., `ErrInsufficientFunds`), call `IdempotencyRepo.Release` so clients can cleanly retry without being blocked[cite: 1].
3. **Double-Entry Orchestration**:
   - Generate balanced pairs: 1 Debit entry for `fromWalletId` and 1 Credit entry for `toWalletId` where amounts match exactly[cite: 1, 2].
4. **Validation**: Provide comprehensive unit tests covering happy paths, insufficient funds, self-transfers, currency mismatches, and idempotency collisions[cite: 1, 2].
5. **Compilation & Test Protocol**: Verify compilation with `go vet ./...` and ensure all tests pass with `go test -v ./internal/service/...`[cite: 1].

---

# TASK: Milestone 3 - Service Layer (Business Logic & Double-Entry Orchestration)

### 1. WHAT (Files to Create)
Implement the core financial service interface, transfer orchestration logic with lease-based idempotency handling, wallet retrieval/creation, and thorough unit tests.

#### Target Files:
1. `internal/service/wallet_service.go`
2. `internal/service/wallet_service_test.go`

---

### 2. WHY
- **Decoupling**: Decouples business rules (currency alignment, positive balance rules, double-entry symmetry) from HTTP parsing and SQL execution[cite: 1, 2].
- **Double-Entry Correctness**: Guarantees that balance modifications are strictly accompanied by paired, immutable audit ledger entries[cite: 1, 2].
- **Safe Concurrent Idempotency**: Prevents in-flight race conditions and eliminates double-charging using two-phase reservation without locking the database across network boundaries[cite: 1].
- **Payload Tampering Protection**: SHA-256 payload hashing prevents malicious or erroneous reuse of the same idempotency key with differing amounts or destinations[cite: 1, 2].

---

### 3. WHEN
Execute immediately after **Milestone 2**[cite: 1] (when repository interfaces, SQLite transaction manager, and schema migrations are compiled and verified).

---

### 4. HOW (Exact File Implementations)

#### Step 4.1: Create `internal/service/wallet_service.go`

```go
package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"[github.com/google/uuid](https://github.com/google/uuid)"
	"[github.com/Robustrade/wallet-transfer-assignment/internal/domain](https://github.com/Robustrade/wallet-transfer-assignment/internal/domain)"
	"[github.com/Robustrade/wallet-transfer-assignment/internal/repository](https://github.com/Robustrade/wallet-transfer-assignment/internal/repository)"
)

// TransferRequest represents the transfer payload matching ASSIGNMENT.md
type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"` // Minor units (e.g. cents)
}

// TransferResponse defines the standard payload returned to clients
type TransferResponse struct {
	TransferID string `json:"transferId"`
	Status     string `json:"status"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	CreatedAt  string `json:"createdAt"`
}

type WalletService interface {
	CreateWallet(ctx context.Context, userID, currency string) (*domain.Wallet, error)
	GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error)
	Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error)
}

type walletService struct {
	txManager       repository.TxManager
	walletRepo      repository.WalletRepository
	transferRepo    repository.TransferRepository
	idempotencyRepo repository.IdempotencyRepository
}

func NewWalletService(
	txManager repository.TxManager,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	idempotencyRepo repository.IdempotencyRepository,
) WalletService {
	return &walletService{
		txManager:       txManager,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		idempotencyRepo: idempotencyRepo,
	}
}

func (s *walletService) CreateWallet(ctx context.Context, userID, currency string) (*domain.Wallet, error) {
	if userID == "" || currency == "" {
		return nil, fmt.Errorf("user_id and currency are required")
	}

	wallet := &domain.Wallet{
		ID:        uuid.New().String(),
		UserID:    userID,
		Currency:  currency,
		Balance:   0,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := s.walletRepo.Create(ctx, nil, wallet); err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}

	return wallet, nil
}

func (s *walletService) GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error) {
	if walletID == "" {
		return nil, domain.ErrWalletNotFound
	}
	return s.walletRepo.GetByID(ctx, nil, walletID)
}

func (s *walletService) Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	// 1. Basic Input Validation
	if req.IdempotencyKey == "" {
		return nil, domain.ErrIdempotencyKeyRequired
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return nil, fmt.Errorf("fromWalletId and toWalletId must be provided")
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, domain.ErrSelfTransfer
	}
	if req.Amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}

	// 2. Compute Payload SHA-256 Hash
	payloadBytes := fmt.Sprintf("%s|%s|%d", req.FromWalletID, req.ToWalletID, req.Amount)
	hashSum := sha256.Sum256([]byte(payloadBytes))
	requestHash := hex.EncodeToString(hashSum[:])

	// 3. Phase 1: Reserve Idempotency Lease (15-second TTL)
	lease, err := s.idempotencyRepo.Reserve(ctx, req.IdempotencyKey, requestHash, 15*time.Second)
	if err != nil {
		return nil, err
	}

	// Cached response replay check
	if lease.Status == domain.IdempotencyStatusResolved {
		if lease.RequestHash != requestHash {
			return nil, domain.ErrIdempotencyKeyConflict
		}
		var cachedResp TransferResponse
		if err := json.Unmarshal([]byte(lease.ResponseBody), &cachedResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cached response: %w", err)
		}
		return &cachedResp, nil
	}

	// 4. Phase 2: Execute Atomic Transfer Transaction
	var finalResp *TransferResponse
	txErr := s.txManager.RunInTx(ctx, func(tx *sql.Tx) error {
		// Fetch source and destination wallets
		fromWallet, err := s.walletRepo.GetByID(ctx, tx, req.FromWalletID)
		if err != nil {
			return err
		}
		toWallet, err := s.walletRepo.GetByID(ctx, tx, req.ToWalletID)
		if err != nil {
			return err
		}

		// Currency consistency check
		if fromWallet.Currency != toWallet.Currency {
			return domain.ErrCurrencyMismatch
		}

		// Balance sufficiency check
		if fromWallet.Balance < req.Amount {
			return domain.ErrInsufficientFunds
		}

		now := time.Now().UTC()
		transferID := uuid.New().String()

		// Prepare immutable double-entry ledger records
		debitEntry := &domain.LedgerEntry{
			ID:         uuid.New().String(),
			TransferID: transferID,
			WalletID:   fromWallet.ID,
			Amount:     req.Amount,
			Direction:  domain.DirectionDebit,
			CreatedAt:  now,
		}

		creditEntry := &domain.LedgerEntry{
			ID:         uuid.New().String(),
			TransferID: transferID,
			WalletID:   toWallet.ID,
			Amount:     req.Amount,
			Direction:  domain.DirectionCredit,
			CreatedAt:  now,
		}

		transferRecord := &domain.Transfer{
			ID:             transferID,
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   fromWallet.ID,
			ToWalletID:     toWallet.ID,
			Amount:         req.Amount,
			Currency:       fromWallet.Currency,
			Status:         domain.TransferStatusCompleted,
			CreatedAt:      now,
		}

		// Persist transfer and ledger entries
		if err := s.transferRepo.CreateTransferAndEntries(ctx, tx, transferRecord, debitEntry, creditEntry); err != nil {
			return fmt.Errorf("failed to save transfer and ledger entries: %w", err)
		}

		// Mutate wallet balances
		if err := s.walletRepo.UpdateBalances(ctx, tx, fromWallet.ID, toWallet.ID, req.Amount); err != nil {
			return fmt.Errorf("failed to update wallet balances: %w", err)
		}

		// Prepare final response
		finalResp = &TransferResponse{
			TransferID: transferID,
			Status:     string(domain.TransferStatusCompleted),
			Amount:     req.Amount,
			Currency:   fromWallet.Currency,
			CreatedAt:  now.Format(time.RFC3339),
		}

		respJSON, err := json.Marshal(finalResp)
		if err != nil {
			return fmt.Errorf("failed to serialize response body: %w", err)
		}

		// Commit status as RESOLVED inside the same transaction
		if err := s.idempotencyRepo.Resolve(ctx, tx, req.IdempotencyKey, 200, string(respJSON)); err != nil {
			return fmt.Errorf("failed to resolve idempotency lease: %w", err)
		}

		return nil
	})

	// 5. Cleanup lease on business/validation failure
	if txErr != nil {
		_ = s.idempotencyRepo.Release(ctx, req.IdempotencyKey)
		return nil, txErr
	}

	return finalResp, nil
}

```

---

#### Step 4.2: Create `internal/service/wallet_service_test.go`

```go
package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"[github.com/stretchr/testify/assert](https://github.com/stretchr/testify/assert)"
	"[github.com/stretchr/testify/mock](https://github.com/stretchr/testify/mock)"
	"[github.com/Robustrade/wallet-transfer-assignment/internal/domain](https://github.com/Robustrade/wallet-transfer-assignment/internal/domain)"
	"[github.com/Robustrade/wallet-transfer-assignment/internal/service](https://github.com/Robustrade/wallet-transfer-assignment/internal/service)"
)

// MockTxManager mocks transaction execution
type MockTxManager struct {
	mock.Mock
}

func (m *MockTxManager) RunInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	args := m.Called(ctx, mock.Anything)
	if args.Get(0) != nil {
		return args.Error(0)
	}
	return fn(nil)
}

// MockWalletRepo mocks wallet storage
type MockWalletRepo struct {
	mock.Mock
}

func (m *MockWalletRepo) Create(ctx context.Context, tx *sql.Tx, wallet *domain.Wallet) error {
	return m.Called(ctx, tx, wallet).Error(0)
}

func (m *MockWalletRepo) GetByID(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error) {
	args := m.Called(ctx, tx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockWalletRepo) UpdateBalances(ctx context.Context, tx *sql.Tx, fromID, toID string, amount int64) error {
	return m.Called(ctx, tx, fromID, toID, amount).Error(0)
}

// MockTransferRepo mocks transfer & ledger entry storage
type MockTransferRepo struct {
	mock.Mock
}

func (m *MockTransferRepo) CreateTransferAndEntries(
	ctx context.Context,
	tx *sql.Tx,
	transfer *domain.Transfer,
	debit *domain.LedgerEntry,
	credit *domain.LedgerEntry,
) error {
	return m.Called(ctx, tx, transfer, debit, credit).Error(0)
}

// MockIdempotencyRepo mocks the lease-based idempotency store
type MockIdempotencyRepo struct {
	mock.Mock
}

func (m *MockIdempotencyRepo) Get(ctx context.Context, tx *sql.Tx, key string) (*domain.IdempotencyRecord, error) {
	args := m.Called(ctx, tx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.IdempotencyRecord), args.Error(1)
}

func (m *MockIdempotencyRepo) Reserve(ctx context.Context, key, requestHash string, ttl time.Duration) (*domain.IdempotencyRecord, error) {
	args := m.Called(ctx, key, requestHash, ttl)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.IdempotencyRecord), args.Error(1)
}

func (m *MockIdempotencyRepo) Resolve(ctx context.Context, tx *sql.Tx, key string, statusCode int, responseBody string) error {
	return m.Called(ctx, tx, key, statusCode, responseBody).Error(0)
}

func (m *MockIdempotencyRepo) Release(ctx context.Context, key string) error {
	return m.Called(ctx, key).Error(0)
}

// --- UNIT TESTS ---

func TestTransfer_Success(t *testing.T) {
	txManager := new(MockTxManager)
	walletRepo := new(MockWalletRepo)
	transferRepo := new(MockTransferRepo)
	idempotencyRepo := new(MockIdempotencyRepo)

	svc := service.NewWalletService(txManager, walletRepo, transferRepo, idempotencyRepo)

	ctx := context.Background()
	req := service.TransferRequest{
		IdempotencyKey: "test-key-1",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         500,
	}

	idempotencyRepo.On("Reserve", ctx, req.IdempotencyKey, mock.Anything, 15*time.Second).
		Return(&domain.IdempotencyRecord{Status: domain.IdempotencyStatusInProgress}, nil)

	txManager.On("RunInTx", ctx, mock.Anything).Return(nil)

	w1 := &domain.Wallet{ID: "w1", Currency: "USD", Balance: 1000}
	w2 := &domain.Wallet{ID: "w2", Currency: "USD", Balance: 200}

	walletRepo.On("GetByID", ctx, mock.Anything, "w1").Return(w1, nil)
	walletRepo.On("GetByID", ctx, mock.Anything, "w2").Return(w2, nil)

	transferRepo.On("CreateTransferAndEntries", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	walletRepo.On("UpdateBalances", ctx, mock.Anything, "w1", "w2", int64(500)).
		Return(nil)

	idempotencyRepo.On("Resolve", ctx, mock.Anything, req.IdempotencyKey, 200, mock.Anything).
		Return(nil)

	resp, err := svc.Transfer(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, int64(500), resp.Amount)
	assert.Equal(t, "COMPLETED", resp.Status)
}

func TestTransfer_InsufficientFunds(t *testing.T) {
	txManager := new(MockTxManager)
	walletRepo := new(MockWalletRepo)
	transferRepo := new(MockTransferRepo)
	idempotencyRepo := new(MockIdempotencyRepo)

	svc := service.NewWalletService(txManager, walletRepo, transferRepo, idempotencyRepo)

	ctx := context.Background()
	req := service.TransferRequest{
		IdempotencyKey: "test-key-2",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         5000,
	}

	idempotencyRepo.On("Reserve", ctx, req.IdempotencyKey, mock.Anything, 15*time.Second).
		Return(&domain.IdempotencyRecord{Status: domain.IdempotencyStatusInProgress}, nil)

	txManager.On("RunInTx", ctx, mock.Anything).Return(nil)

	w1 := &domain.Wallet{ID: "w1", Currency: "USD", Balance: 500} // Less than 5000
	w2 := &domain.Wallet{ID: "w2", Currency: "USD", Balance: 200}

	walletRepo.On("GetByID", ctx, mock.Anything, "w1").Return(w1, nil)
	walletRepo.On("GetByID", ctx, mock.Anything, "w2").Return(w2, nil)
	idempotencyRepo.On("Release", ctx, req.IdempotencyKey).Return(nil)

	resp, err := svc.Transfer(ctx, req)

	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)
	assert.Nil(t, resp)
}

func TestTransfer_CurrencyMismatch(t *testing.T) {
	txManager := new(MockTxManager)
	walletRepo := new(MockWalletRepo)
	transferRepo := new(MockTransferRepo)
	idempotencyRepo := new(MockIdempotencyRepo)

	svc := service.NewWalletService(txManager, walletRepo, transferRepo, idempotencyRepo)

	ctx := context.Background()
	req := service.TransferRequest{
		IdempotencyKey: "test-key-3",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100,
	}

	idempotencyRepo.On("Reserve", ctx, req.IdempotencyKey, mock.Anything, 15*time.Second).
		Return(&domain.IdempotencyRecord{Status: domain.IdempotencyStatusInProgress}, nil)

	txManager.On("RunInTx", ctx, mock.Anything).Return(nil)

	w1 := &domain.Wallet{ID: "w1", Currency: "USD", Balance: 1000}
	w2 := &domain.Wallet{ID: "w2", Currency: "EUR", Balance: 200} // EUR != USD

	walletRepo.On("GetByID", ctx, mock.Anything, "w1").Return(w1, nil)
	walletRepo.On("GetByID", ctx, mock.Anything, "w2").Return(w2, nil)
	idempotencyRepo.On("Release", ctx, req.IdempotencyKey).Return(nil)

	resp, err := svc.Transfer(ctx, req)

	assert.ErrorIs(t, err, domain.ErrCurrencyMismatch)
	assert.Nil(t, resp)
}

func TestTransfer_SelfTransfer(t *testing.T) {
	svc := service.NewWalletService(nil, nil, nil, nil)

	req := service.TransferRequest{
		IdempotencyKey: "test-key-4",
		FromWalletID:   "w1",
		ToWalletID:     "w1", // Self transfer
		Amount:         100,
	}

	resp, err := svc.Transfer(context.Background(), req)

	assert.ErrorIs(t, err, domain.ErrSelfTransfer)
	assert.Nil(t, resp)
}

func TestTransfer_CachedReplay(t *testing.T) {
	idempotencyRepo := new(MockIdempotencyRepo)
	svc := service.NewWalletService(nil, nil, nil, idempotencyRepo)

	ctx := context.Background()
	req := service.TransferRequest{
		IdempotencyKey: "cached-key",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100,
	}

	cachedBody := `{"transferId":"tx-123","status":"COMPLETED","amount":100,"currency":"USD","createdAt":"2026-10-25T00:00:00Z"}`

	idempotencyRepo.On("Reserve", ctx, req.IdempotencyKey, mock.Anything, 15*time.Second).
		Return(&domain.IdempotencyRecord{
			Status:       domain.IdempotencyStatusResolved,
			RequestHash:  "739d67566236b28373b5e4f4fb2bc7433ecbc6173a1158ef492067759dd1d7fa", // Matches "w1|w2|100"
			ResponseBody: cachedBody,
		}, nil)

	resp, err := svc.Transfer(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "tx-123", resp.TransferID)
	assert.Equal(t, int64(100), resp.Amount)
}

func TestTransfer_IdempotencyHashMismatch(t *testing.T) {
	idempotencyRepo := new(MockIdempotencyRepo)
	svc := service.NewWalletService(nil, nil, nil, idempotencyRepo)

	ctx := context.Background()
	req := service.TransferRequest{
		IdempotencyKey: "conflict-key",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100,
	}

	idempotencyRepo.On("Reserve", ctx, req.IdempotencyKey, mock.Anything, 15*time.Second).
		Return(&domain.IdempotencyRecord{
			Status:      domain.IdempotencyStatusResolved,
			RequestHash: "different-unmatched-hash",
		}, nil)

	resp, err := svc.Transfer(ctx, req)

	assert.ErrorIs(t, err, domain.ErrIdempotencyKeyConflict)
	assert.Nil(t, resp)
}

```

---

### 5. CLI EXECUTION & VERIFICATION PROTOCOL

Run these validation commands to verify the setup:

1. Tidy modules and verify mock dependencies:

```bash
go mod tidy

```

2. Run compiler type check and syntax vetting:

```bash
go vet ./...

```

3. Run the unit test suite:

```bash
go test -v ./internal/service/...

```

**Success Criteria**:

* `go vet ./...` completes with 0 warnings or errors.
* `go test -v ./internal/service/...` outputs all test passes (`PASS`) with exit code 0.
* Proper error releases and two-phase idempotency checks execute cleanly without test panics.
