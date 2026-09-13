package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/ankit729/wallet-transfer/internal/domain"
)

type Queue interface {
	Publish(context.Context, domain.TransferEvent) error
}

type Store interface {
	Ping(context.Context) error

	CreateWallet(
		context.Context,
		domain.CreateWalletRequest,
	) (domain.Wallet, error)

	GetWallet(
		context.Context,
		string,
	) (domain.Wallet, error)

	CreateTransfer(
		context.Context,
		domain.TransferRequest,
		string,
	) (domain.TransferResult, *domain.TransferEvent, error)

	ProcessDebit(context.Context, domain.TransferEvent) (domain.TransferEvent, error)
	ProcessCredit(context.Context, domain.TransferEvent) error

	GetTransfer(
		context.Context,
		string,
	) (domain.TransferResult, error)
}

type WalletCache interface {
	GetWallet(
		context.Context,
		string,
	) (domain.Wallet, bool, error)

	SetWallet(
		context.Context,
		domain.Wallet,
	) error

	DeleteWallet(
		context.Context,
		string,
	) error
}

type Service struct {
	store Store
	cache WalletCache
	queue Queue
}

func New(store Store, cache WalletCache, queues ...Queue) *Service {
	var queue Queue
	if len(queues) > 0 {
		queue = queues[0]
	}

	return &Service{
		store: store,
		cache: cache,
		queue: queue,
	}
}

func (s *Service) Ping(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) CreateWallet(
	ctx context.Context,
	request domain.CreateWalletRequest,
) (domain.Wallet, error) {
	if err := request.Validate(); err != nil {
		return domain.Wallet{}, err
	}

	wallet, err := s.store.CreateWallet(ctx, request)
	if err != nil {
		return domain.Wallet{}, err
	}

	if s.cache != nil {
		_ = s.cache.SetWallet(ctx, wallet)
	}

	return wallet, nil
}

func (s *Service) GetWallet(
	ctx context.Context,
	walletID string,
) (domain.Wallet, error) {
	if s.cache != nil {
		if wallet, hit, err := s.cache.GetWallet(ctx, walletID); err == nil && hit {
			return wallet, nil
		}
	}

	wallet, err := s.store.GetWallet(ctx, walletID)
	if err != nil {
		return domain.Wallet{}, err
	}

	if s.cache != nil {
		_ = s.cache.SetWallet(ctx, wallet)
	}

	return wallet, nil
}

func (s *Service) CreateTransfer(
	ctx context.Context,
	request domain.TransferRequest,
) (domain.TransferResult, error) {
	if err := request.Validate(); err != nil {
		return domain.TransferResult{}, err
	}

	requestHash := hashTransferRequest(request)

	result, event, err := s.store.CreateTransfer(
		ctx,
		request,
		requestHash,
	)
	if err != nil {
		return domain.TransferResult{}, err
	}

	if event != nil && s.queue != nil {
		err = s.queue.Publish(ctx, *event)
		if err != nil {
			return domain.TransferResult{}, err
		}
	}
	if event != nil && s.queue == nil {
		next, processErr := s.store.ProcessDebit(ctx, *event)
		if processErr != nil {
			return domain.TransferResult{}, processErr
		}
		if next.Type != "" {
			if processErr = s.store.ProcessCredit(ctx, next); processErr != nil {
				return domain.TransferResult{}, processErr
			}
		}
		result, err = s.store.GetTransfer(ctx, result.ID)
		if err != nil {
			return domain.TransferResult{}, err
		}
	}

	if result.Status == domain.TransferProcessed && s.cache != nil {
		_ = s.cache.DeleteWallet(ctx, request.FromWalletID)
		_ = s.cache.DeleteWallet(ctx, request.ToWalletID)
	}

	return result, nil
}

func (s *Service) ProcessEvent(ctx context.Context, event domain.TransferEvent) error {
	switch event.Type {
	case domain.DebitWalletEvent:
		next, err := s.store.ProcessDebit(ctx, event)
		if err != nil {
			return err
		}
		if s.queue != nil && next.Type != "" {
			return s.queue.Publish(ctx, next)
		}
		return nil
	case domain.CreditWalletEvent:
		if err := s.store.ProcessCredit(ctx, event); err != nil {
			return err
		}
		if s.cache != nil {
			result, err := s.store.GetTransfer(ctx, event.TransferID)
			if err != nil {
				return err
			}
			_ = s.cache.DeleteWallet(ctx, result.FromWalletID)
			_ = s.cache.DeleteWallet(ctx, result.ToWalletID)
		}
		return nil
	default:
		return fmt.Errorf("unsupported transfer event type %q", event.Type)
	}
}

func (s *Service) GetTransfer(
	ctx context.Context,
	transferID string,
) (domain.TransferResult, error) {
	return s.store.GetTransfer(ctx, transferID)
}

func hashTransferRequest(request domain.TransferRequest) string {
	payload := request.FromWalletID +
		"\x00" +
		request.ToWalletID +
		"\x00" +
		strconv.FormatInt(request.Amount, 10)

	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}
