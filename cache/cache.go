package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

type Cache interface {
	// Get a value from the cache and return true if found, any is the value if found and nil if no error.
	Get(key string) (bool, any, error)

	// Set a value into the cache with a cache expiration.
	Set(key string, val any, expires time.Duration) error

	// Hits returns the number of times a key has been accessed.
	Hits(key string) (bool, int)

	// Expire will expire a key in the cache.
	Expire(key string) (bool, error)

	// Close will shutdown the cache.
	Close() error
}

type value struct {
	object  any
	expires time.Time
	hits    int
}

// Get retrieves a typed value from the cache.
// For in-memory caches, it performs a direct type assertion.
// For serialized caches (like SQLite), it deserializes from []byte using msgpack.
func Get[T any](c Cache, key string) (bool, T, error) {
	found, val, err := c.Get(key)
	if !found || err != nil {
		var zero T
		return false, zero, err
	}
	// Direct type assertion (works for in-memory cache)
	if typed, ok := val.(T); ok {
		return true, typed, nil
	}
	// Deserialize from []byte (works for serialized caches like SQLite)
	if data, ok := val.([]byte); ok {
		var result T
		if err := msgpack.Unmarshal(data, &result); err != nil {
			var zero T
			return false, zero, fmt.Errorf("cache: failed to unmarshal value: %w", err)
		}
		return true, result, nil
	}
	var zero T
	return false, zero, fmt.Errorf("cache: cannot convert value of type %T to %T", val, zero)
}

// DefaultExpires is the default TTL used by Exec when CacheConfig.Expires is zero.
const DefaultExpires = 5 * time.Minute

// CacheConfig configures the Exec helper.
type CacheConfig struct {
	// Expires is the TTL for cached values. Defaults to DefaultExpires if zero.
	Expires time.Duration
	// Key is the cache key. Required.
	Key string
}

// Invoker is a function that produces a value of type T.
// The bool return indicates whether a value was found. Return false to signal
// "not found" without caching a zero value (e.g. sql.ErrNoRows scenarios).
type Invoker[T any] func(ctx context.Context) (T, bool, error)

// Exec is a cache-aside helper. It checks the cache for config.Key first.
// On a cache hit, it returns the cached value with found=true.
// On a cache miss, it calls invoke to produce the value. If invoke returns
// found=true, the value is stored in the cache and returned with found=true.
// If invoke returns found=false, nothing is cached and found=false is returned.
// If invoke returns an error, nothing is cached and the error is propagated.
// If the cache Set fails after a successful invoke, the value is still
// returned (the Set error is swallowed since the primary operation succeeded).
func Exec[T any](ctx context.Context, config CacheConfig, c Cache, invoke Invoker[T]) (bool, T, error) {
	// Try cache first.
	found, val, err := Get[T](c, config.Key)
	if err == nil && found {
		return true, val, nil
	}

	// Cache miss (or cache read error) — invoke the function.
	result, ok, err := invoke(ctx)
	if err != nil {
		var zero T
		return false, zero, err
	}

	// Invoker said "not found" — do not cache.
	if !ok {
		var zero T
		return false, zero, nil
	}

	// Store in cache. Swallow Set errors — the caller got their value.
	expires := config.Expires
	if expires <= 0 {
		expires = DefaultExpires
	}
	_ = c.Set(config.Key, result, expires)

	return true, result, nil
}
