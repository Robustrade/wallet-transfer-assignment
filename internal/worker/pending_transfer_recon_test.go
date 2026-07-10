package worker_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/testutils"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestPendingTransferRecon_ResolvesEveryPendingTransfer(t *testing.T) {
	ctx := context.Background()
	mockRepo := &testutils.MockITransferRepository{}

	pending := []*domain.Transfer{
		{ID: "txn-1", Status: domain.TransferPending},
		{ID: "txn-2", Status: domain.TransferPending},
	}
	mockRepo.On("ListPendingTransfers", ctx).Return(pending, nil)
	mockRepo.On("ResolvePendingTransfer", ctx, pending[0]).Return(&domain.Transfer{ID: "txn-1", Status: domain.TransferProcessed}, nil)
	mockRepo.On("ResolvePendingTransfer", ctx, pending[1]).Return(&domain.Transfer{ID: "txn-2", Status: domain.TransferFailed}, nil)

	recon := &worker.PendingTransferRecon{TransferRepo: mockRepo}
	recon.RunOnce(ctx)

	mockRepo.AssertExpectations(t)
}

func TestPendingTransferRecon_ContinuesAfterOneResolveFails(t *testing.T) {
	ctx := context.Background()
	mockRepo := &testutils.MockITransferRepository{}

	pending := []*domain.Transfer{
		{ID: "txn-1", Status: domain.TransferPending},
		{ID: "txn-2", Status: domain.TransferPending},
	}
	mockRepo.On("ListPendingTransfers", ctx).Return(pending, nil)
	mockRepo.On("ResolvePendingTransfer", ctx, pending[0]).Return(nil, errors.New("connection reset"))
	mockRepo.On("ResolvePendingTransfer", ctx, pending[1]).Return(&domain.Transfer{ID: "txn-2", Status: domain.TransferProcessed}, nil)

	recon := &worker.PendingTransferRecon{TransferRepo: mockRepo}
	recon.RunOnce(ctx)

	// Both calls must have happened - one failing must not stop the rest
	// of the batch from being attempted.
	mockRepo.AssertExpectations(t)
}

func TestPendingTransferRecon_NoPendingTransfers_DoesNotCallResolve(t *testing.T) {
	ctx := context.Background()
	mockRepo := &testutils.MockITransferRepository{}
	mockRepo.On("ListPendingTransfers", ctx).Return([]*domain.Transfer{}, nil)

	recon := &worker.PendingTransferRecon{TransferRepo: mockRepo}
	recon.RunOnce(ctx)

	mockRepo.AssertNotCalled(t, "ResolvePendingTransfer", mock.Anything, mock.Anything)
	mockRepo.AssertExpectations(t)
}

func TestPendingTransferRecon_ListError_DoesNotPanic(t *testing.T) {
	ctx := context.Background()
	mockRepo := &testutils.MockITransferRepository{}
	mockRepo.On("ListPendingTransfers", ctx).Return(nil, errors.New("db unreachable"))

	recon := &worker.PendingTransferRecon{TransferRepo: mockRepo}
	assert.NotPanics(t, func() { recon.RunOnce(ctx) })

	mockRepo.AssertNotCalled(t, "ResolvePendingTransfer", mock.Anything, mock.Anything)
}

// A run still in progress must cause a concurrent/overlapping run to skip
// entirely rather than executing alongside it - simulated here by blocking
// ListPendingTransfers on a channel until the second call has already had
// a chance to observe the reconciler as busy and bail out.
func TestPendingTransferRecon_SkipsOverlappingRun(t *testing.T) {
	ctx := context.Background()
	mockRepo := &testutils.MockITransferRepository{}

	release := make(chan struct{})
	started := make(chan struct{})
	mockRepo.On("ListPendingTransfers", ctx).
		Run(func(args mock.Arguments) {
			close(started)
			<-release
		}).
		Return([]*domain.Transfer{}, nil).
		Once()

	recon := &worker.PendingTransferRecon{TransferRepo: mockRepo}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		recon.RunOnce(ctx)
	}()

	<-started
	// The first run is now blocked inside ListPendingTransfers, holding
	// the "running" flag. A second run must observe that and return
	// immediately without calling ListPendingTransfers again.
	recon.RunOnce(ctx)

	close(release)
	wg.Wait()

	mockRepo.AssertExpectations(t) // ListPendingTransfers called exactly Once
}
