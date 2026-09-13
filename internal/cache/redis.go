package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ankit729/wallet-transfer/internal/domain"
	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
}

func New(ctx context.Context, redisURL string, ttl time.Duration) (*RedisCache, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(options)

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}

	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	return &RedisCache{
		client: client,
		ttl:    ttl,
	}, nil
}

func (c *RedisCache) Close() error {
	return c.client.Close()
}

func (c *RedisCache) key(walletID string) string {
	return "wallet:" + walletID
}

func (c *RedisCache) GetWallet(
	ctx context.Context,
	walletID string,
) (domain.Wallet, bool, error) {
	value, err := c.client.Get(ctx, c.key(walletID)).Bytes()
	if err == redis.Nil {
		return domain.Wallet{}, false, nil
	}
	if err != nil {
		return domain.Wallet{}, false, err
	}

	var wallet domain.Wallet
	if err := json.Unmarshal(value, &wallet); err != nil {
		return domain.Wallet{}, false, err
	}

	return wallet, true, nil
}

func (c *RedisCache) SetWallet(
	ctx context.Context,
	wallet domain.Wallet,
) error {
	value, err := json.Marshal(wallet)
	if err != nil {
		return err
	}

	return c.client.Set(
		ctx,
		c.key(wallet.ID),
		value,
		c.ttl,
	).Err()
}

func (c *RedisCache) DeleteWallet(
	ctx context.Context,
	walletID string,
) error {
	return c.client.Del(ctx, c.key(walletID)).Err()
}
