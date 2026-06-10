package application

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthService struct {
	pool DBPool
}

type DBPool interface {
	Ping(ctx context.Context) error
	Stat() DBPoolStat
}

type DBPoolStat interface {
	TotalConns() int32
	IdleConns() int32
	AcquiredConns() int32
	MaxConns() int32
	AcquireCount() int64
	EmptyAcquireCount() int64
	EmptyAcquireWaitTime() time.Duration
}

type pgxPoolAdapter struct {
	pool *pgxpool.Pool
}

func (p *pgxPoolAdapter) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *pgxPoolAdapter) Stat() DBPoolStat {
	return p.pool.Stat()
}

type HealthCheck struct {
	Name    string                 `json:"name"`
	Status  string                 `json:"status"`
	Details map[string]interface{} `json:"details,omitempty"`
}

type HealthResponse struct {
	Status string        `json:"status"`
	Checks []HealthCheck `json:"checks,omitempty"`
}

func NewHealthService(pool DBPool) *HealthService {
	return &HealthService{pool: pool}
}

func NewHealthServiceFromPgxPool(pool *pgxpool.Pool) *HealthService {
	return NewHealthService(&pgxPoolAdapter{pool: pool})
}

func (s *HealthService) Live(ctx context.Context) HealthResponse {
	return HealthResponse{Status: "UP"}
}

func (s *HealthService) Ready(ctx context.Context) (*HealthResponse, error) {
	if s.pool == nil {
		response := &HealthResponse{
			Status: "DOWN",
			Checks: []HealthCheck{{
				Name:   "postgres",
				Status: "DOWN",
				Details: map[string]interface{}{
					"error": "database pool is not configured",
				},
			}},
		}
		return response, fmt.Errorf("database pool is not configured")
	}

	stats := s.pool.Stat()
	check := HealthCheck{
		Name:   "postgres",
		Status: "UP",
		Details: map[string]interface{}{
			"totalConnections":     stats.TotalConns(),
			"idleConnections":      stats.IdleConns(),
			"acquiredConnections":  stats.AcquiredConns(),
			"maxConnections":       stats.MaxConns(),
			"acquireCount":         stats.AcquireCount(),
			"emptyAcquireCount":    stats.EmptyAcquireCount(),
			"emptyAcquireWaitTime": stats.EmptyAcquireWaitTime().String(),
		},
	}

	if err := s.pool.Ping(ctx); err != nil {
		check.Status = "DOWN"
		check.Details["error"] = err.Error()
		return &HealthResponse{Status: "DOWN", Checks: []HealthCheck{check}}, err
	}

	return &HealthResponse{Status: "UP", Checks: []HealthCheck{check}}, nil
}
