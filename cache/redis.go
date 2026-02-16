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
	prefix string
}

var _ Cache = (*redisCache)(nil)

// NewRedis returns a new Cache backed by Redis.
// The caller owns the redis.Client lifecycle — Close is a no-op on the client.
// prefix is prepended to all keys for namespacing (can be empty).
func NewRedis(ctx context.Context, client *redis.Client, prefix string) Cache {
	return &redisCache{
		client: client,
		ctx:    ctx,
		prefix: prefix,
	}
}

func (c *redisCache) prefixKey(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + ":" + key
}

func (c *redisCache) Get(key string) (bool, any, error) {
	k := c.prefixKey(key)
	data, err := c.client.HGet(c.ctx, k, "v").Bytes()
	if err == redis.Nil {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	// Increment hits (fire-and-forget, don't fail the Get).
	c.client.HIncrBy(c.ctx, k, "h", 1)
	return true, data, nil
}

func (c *redisCache) Set(key string, val any, expires time.Duration) error {
	data, err := msgpack.Marshal(val)
	if err != nil {
		return err
	}
	k := c.prefixKey(key)
	pipe := c.client.Pipeline()
	pipe.HSet(c.ctx, k, "v", data, "h", 0)
	pipe.Expire(c.ctx, k, expires)
	_, err = pipe.Exec(c.ctx)
	return err
}

func (c *redisCache) Hits(key string) (bool, int) {
	hits, err := c.client.HGet(c.ctx, c.prefixKey(key), "h").Int()
	if err != nil {
		return false, 0
	}
	return true, hits
}

func (c *redisCache) Expire(key string) (bool, error) {
	result, err := c.client.Del(c.ctx, c.prefixKey(key)).Result()
	if err != nil {
		return false, err
	}
	return result > 0, nil
}

// Close is a no-op — the caller owns the redis.Client lifecycle.
func (c *redisCache) Close() error {
	return nil
}
