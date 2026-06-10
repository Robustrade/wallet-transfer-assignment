package application

import (
	"context"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/observability"
	repository "github.com/Robustrade/wallet-transfer-assignment/internal/respository"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestTransferService_Execute_IncrementsIdempotencyHitMetrics(t *testing.T) {
	metrics := observability.NewMetrics(prometheus.NewRegistry())

	transferRepo := &mockTransferRepo{
		getByIdempotencyKeyFn: func(ctx context.Context, key string) (*domain.Transfer, error) {
			return &domain.Transfer{ID: "transfer-2", Status: domain.TransferStatusProcessed}, nil
		},
		getByIDFn: func(ctx context.Context, transferID string) (*domain.Transfer, error) {
			return &domain.Transfer{ID: transferID, Status: domain.TransferStatusProcessed}, nil
		},
	}
	idempotencyRepo := &mockIdempotencyRepo{
		getByKeyFn: func(ctx context.Context, key string) (*repository.IdempotencyRecord, error) {
			return &repository.IdempotencyRecord{IdempotencyKey: key, TransferID: "transfer-2", RequestHash: "hash-1"}, nil
		},
	}
	service := NewTransferService(&mockTransactionManager{}, nil, transferRepo, nil, idempotencyRepo, metrics)

	req := TransferRequest{TransferID: "transfer-2", IdempotencyKey: "idem-2", RequestHash: "hash-1"}
	_, err := service.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got := getCounterValue(t, metrics.Registry(), "wallet_transfer_idempotency_hits_total", nil); got != 1 {
		t.Fatalf("expected 1 idempotency hit, got %f", got)
	}
	if got := getHistogramCount(t, metrics.Registry(), "wallet_transfer_transfer_duration_seconds", nil); got != 1 {
		t.Fatalf("expected 1 transfer duration observation, got %d", got)
	}
}

func getCounterValue(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if labelsMatch(metric, labels) {
				return metric.GetCounter().GetValue()
			}
		}
	}
	t.Fatalf("counter metric %s not found", name)
	return 0
}

func getHistogramCount(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) int {
	t.Helper()
	mfs, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if labelsMatch(metric, labels) {
				return int(metric.GetHistogram().GetSampleCount())
			}
		}
	}
	t.Fatalf("histogram metric %s not found", name)
	return 0
}

func labelsMatch(metric *dto.Metric, labels map[string]string) bool {
	for key, value := range labels {
		found := false
		for _, label := range metric.GetLabel() {
			if label.GetName() == key && label.GetValue() == value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
