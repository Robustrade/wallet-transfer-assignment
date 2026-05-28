package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	httpadapter "wallet-transfer-assignment/internal/adapters/http"
	pgadapter "wallet-transfer-assignment/internal/adapters/postgres"
	"wallet-transfer-assignment/internal/cache"
	"wallet-transfer-assignment/internal/idempotency"
	"wallet-transfer-assignment/internal/platform/config"
	"wallet-transfer-assignment/internal/platform/httputil"
	"wallet-transfer-assignment/internal/transfers"
	"wallet-transfer-assignment/internal/wallets"
)

type App struct {
	cfg   config.Config
	pool  *pgxpool.Pool
	http  *http.Server
	stopC chan error
	close func(context.Context) error
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}

	if err := waitForDB(ctx, pool, 15*time.Second); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db not ready: %w", err)
	}

	walletRepo := pgadapter.NewWalletRepository(pool)
	transferRepo := pgadapter.NewTransferRepository(pool)
	ledgerRepo := pgadapter.NewLedgerRepository(pool)
	idempoRepo := pgadapter.NewIdempotencyRepository(pool)

	idempoCache := cache.NewLRUTTL[idempotency.Key, idempotency.StoredResponse](
		cache.WithMaxEntries(10_000),
		cache.WithDefaultTTL(10*time.Minute),
	)
	cachedIdempo := idempotency.NewCachedStore(idempoRepo, idempoCache)

	transferSvc := transfers.NewService(transfers.ServiceDeps{
		Wallets:     walletRepo,
		Transfers:   transferRepo,
		Ledger:      ledgerRepo,
		Idempotency: cachedIdempo,
		DB:          pool,
	})

	walletSvc := wallets.NewService(wallets.ServiceDeps{
		Wallets: walletRepo,
	})

	router := newRouter(transferSvc, walletSvc)
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{
		cfg:   cfg,
		pool:  pool,
		http:  server,
		stopC: make(chan error, 1),
		close: func(ctx context.Context) error {
			pool.Close()
			return nil
		},
	}, nil
}

// Stop triggers a graceful shutdown. It is safe to call multiple times.
// The first non-nil error passed will be returned by Run.
func (a *App) Stop(err error) {
	select {
	case a.stopC <- err:
	default:
	}
}

func (a *App) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		err := a.http.ListenAndServe()
		if err == nil {
			return nil
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			// context cancelled via signal or another goroutine error
		case err := <-a.stopC:
			// internal shutdown requested
			if err != nil {
				return err
			}
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = a.http.Shutdown(shutdownCtx)
		_ = a.close(shutdownCtx)
		return nil
	})

	// DB health monitor: if the DB becomes unreachable at runtime, trigger shutdown.
	// This is intentionally conservative: a few consecutive failures indicates loss of DB dependency.
	g.Go(func() error {
		const (
			interval   = 5 * time.Second
			maxFailure = 3
		)
		t := time.NewTicker(interval)
		defer t.Stop()

		failures := 0
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
				pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				err := a.pool.Ping(pingCtx)
				cancel()
				if err == nil {
					failures = 0
					continue
				}
				failures++
				if failures >= maxFailure {
					a.Stop(fmt.Errorf("database unreachable: %w", err))
					return fmt.Errorf("database unreachable: %w", err)
				}
			}
		}
	})

	if err := g.Wait(); err != nil {
		// Best-effort cleanup even if server fails before context cancellation.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = a.http.Shutdown(shutdownCtx)
		_ = a.close(shutdownCtx)
		return err
	}
	return nil
}

func newRouter(transferSvc transfers.Service, walletSvc *wallets.Service) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(httputil.JSONContentType)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	httpadapter.RegisterRoutes(r, httpadapter.Deps{
		Transfers: transferSvc,
		Wallets:   walletSvc,
	})
	return r
}

func waitForDB(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := pool.Ping(ctx); err == nil {
			return nil
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("timed out after %s", timeout)
}
