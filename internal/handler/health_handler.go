package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const healthCheckTimeout = 2 * time.Second

// HealthHandler serves GET /health by checking Postgres and Redis
// reachability directly, independent of any other middleware.
type HealthHandler struct {
	db    *pgxpool.Pool
	redis *redis.Client
}

// NewHealthHandler constructs a HealthHandler for the given dependencies.
func NewHealthHandler(db *pgxpool.Pool, redis *redis.Client) *HealthHandler {
	return &HealthHandler{db: db, redis: redis}
}

// Health returns 200 when both Postgres and Redis are reachable, 503
// otherwise, reporting per-dependency status in the body. The two
// checks run concurrently so the worst-case latency is bounded by the
// slower of the two, not their sum.
func (h *HealthHandler) Health(c *gin.Context) {
	var postgresOK, redisOK bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		postgresOK = h.checkPostgres(c.Request.Context())
	}()
	go func() {
		defer wg.Done()
		redisOK = h.checkRedis(c.Request.Context())
	}()
	wg.Wait()

	postgresStatus, redisStatus := "ok", "ok"
	if !postgresOK {
		postgresStatus = "error"
	}
	if !redisOK {
		redisStatus = "error"
	}

	if !postgresOK || !redisOK {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":   "unavailable",
			"postgres": postgresStatus,
			"redis":    redisStatus,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"postgres": postgresStatus,
		"redis":    redisStatus,
	})
}

func (h *HealthHandler) checkPostgres(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	var result int
	return h.db.QueryRow(ctx, "SELECT 1").Scan(&result) == nil
}

func (h *HealthHandler) checkRedis(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	return h.redis.Ping(ctx).Err() == nil
}
