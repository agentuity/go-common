package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vmihailenco/msgpack/v5"
)

type redisCache struct {
	client *redis.Client
	ctx    context.Context
	cfg    config
}

var _ Cache = (*redisCache)(nil)

// NewRedis returns a new Cache backed by Redis.
// The caller owns the redis.Client lifecycle — Close is a no-op on the client.
func NewRedis(ctx context.Context, client *redis.Client, opts ...Option) Cache {
	cfg := applyOptions(opts)
	return &redisCache{
		client: client,
		ctx:    ctx,
		cfg:    cfg,
	}
}

func (c *redisCache) queryCtx(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, c.cfg.queryTimeout)
}

func (c *redisCache) prefixKey(key string) string {
	if c.cfg.prefix == "" {
		return key
	}
	return c.cfg.prefix + ":" + key
}

func (c *redisCache) GetContext(ctx context.Context, key string) (bool, any, error) {
	qctx, cancel := c.queryCtx(ctx)
	defer cancel()
	k := c.prefixKey(key)
	data, err := c.client.HGet(qctx, k, "v").Bytes()
	if err == redis.Nil {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	// Increment hits (fire-and-forget, don't fail the Get).
	c.client.HIncrBy(qctx, k, "h", 1)
	return true, data, nil
}

func (c *redisCache) Get(key string) (bool, any, error) {
	return c.GetContext(c.ctx, key)
}

func (c *redisCache) SetContext(ctx context.Context, key string, val any, expires time.Duration) error {
	if expires <= 0 {
		expires = c.cfg.defaultExpires
	}
	data, err := msgpack.Marshal(val)
	if err != nil {
		return err
	}
	qctx, cancel := c.queryCtx(ctx)
	defer cancel()
	k := c.prefixKey(key)
	pipe := c.client.Pipeline()
	pipe.HSet(qctx, k, "v", data, "h", 0)
	pipe.Expire(qctx, k, expires)
	_, err = pipe.Exec(qctx)
	return err
}

func (c *redisCache) Set(key string, val any, expires time.Duration) error {
	return c.SetContext(c.ctx, key, val, expires)
}

func (c *redisCache) HitsContext(ctx context.Context, key string) (bool, int) {
	qctx, cancel := c.queryCtx(ctx)
	defer cancel()
	hits, err := c.client.HGet(qctx, c.prefixKey(key), "h").Int()
	if err != nil {
		return false, 0
	}
	return true, hits
}

func (c *redisCache) Hits(key string) (bool, int) {
	return c.HitsContext(c.ctx, key)
}

func (c *redisCache) ExpireContext(ctx context.Context, key string) (bool, error) {
	qctx, cancel := c.queryCtx(ctx)
	defer cancel()
	result, err := c.client.Del(qctx, c.prefixKey(key)).Result()
	if err != nil {
		return false, err
	}
	return result > 0, nil
}

func (c *redisCache) Expire(key string) (bool, error) {
	return c.ExpireContext(c.ctx, key)
}

// CloseContext is a no-op — the caller owns the redis.Client lifecycle.
func (c *redisCache) CloseContext(_ context.Context) error {
	return nil
}

// Close is a no-op — the caller owns the redis.Client lifecycle.
func (c *redisCache) Close() error {
	return c.CloseContext(c.ctx)
}
