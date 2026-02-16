package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

type Cache interface {
	// Deprecated: Use GetContext instead.
	Get(key string) (bool, any, error)
	// GetContext retrieves a value from the cache. The context controls
	// cancellation and timeout for I/O-backed implementations.
	GetContext(ctx context.Context, key string) (bool, any, error)

	// Deprecated: Use SetContext instead.
	Set(key string, val any, expires time.Duration) error
	// SetContext stores a value in the cache with a TTL. If expires <= 0,
	// the cache's configured default TTL is used.
	SetContext(ctx context.Context, key string, val any, expires time.Duration) error

	// Deprecated: Use HitsContext instead.
	Hits(key string) (bool, int)
	// HitsContext returns the number of times a key has been accessed.
	HitsContext(ctx context.Context, key string) (bool, int)

	// Deprecated: Use ExpireContext instead.
	Expire(key string) (bool, error)
	// ExpireContext removes a key from the cache.
	ExpireContext(ctx context.Context, key string) (bool, error)

	// Deprecated: Use CloseContext instead.
	Close() error
	// CloseContext shuts down the cache.
	CloseContext(ctx context.Context) error
}

type value struct {
	object  any
	expires time.Time
	hits    int
}

// GetContext retrieves a typed value from the cache using the provided context.
// For in-memory caches, it performs a direct type assertion.
// For serialized caches (like SQLite), it deserializes from []byte using msgpack.
func GetContext[T any](ctx context.Context, c Cache, key string) (bool, T, error) {
	found, val, err := c.GetContext(ctx, key)
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

// Deprecated: Use GetContext instead.
func Get[T any](c Cache, key string) (bool, T, error) {
	return GetContext[T](context.Background(), c, key)
}

// DefaultExpires is the default TTL used by Exec when CacheConfig.Expires is zero.
const DefaultExpires = 5 * time.Minute

// DefaultQueryTimeout is the per-operation timeout for cache backends that
// perform I/O (SQLite, Redis). Prevents indefinite hangs on slow or
// unresponsive storage.
const DefaultQueryTimeout = 5 * time.Second

// config holds the resolved configuration for a cache implementation.
type config struct {
	defaultExpires time.Duration
	queryTimeout   time.Duration
	expiryCheck    time.Duration
	prefix         string
}

// Option configures a Cache implementation.
type Option func(*config)

func defaultConfig() config {
	return config{
		defaultExpires: DefaultExpires,
		queryTimeout:   DefaultQueryTimeout,
		expiryCheck:    time.Minute,
	}
}

func applyOptions(opts []Option) config {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithExpires sets the default TTL for cached values. This is used when
// Set is called with expires <= 0. Defaults to DefaultExpires (5 minutes).
func WithExpires(d time.Duration) Option {
	return func(c *config) { c.defaultExpires = d }
}

// WithQueryTimeout sets the per-operation timeout for I/O-backed caches
// (SQLite, Redis). Defaults to DefaultQueryTimeout (5 seconds).
func WithQueryTimeout(d time.Duration) Option {
	return func(c *config) { c.queryTimeout = d }
}

// WithExpiryCheck sets the interval for background expired entry cleanup.
// Applies to InMemory and SQLite backends. Defaults to 1 minute.
func WithExpiryCheck(d time.Duration) Option {
	return func(c *config) { c.expiryCheck = d }
}

// WithPrefix sets the key prefix for namespacing cache keys.
// Applies to the Redis backend. Defaults to empty (no prefix).
func WithPrefix(p string) Option {
	return func(c *config) { c.prefix = p }
}

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
// If invoke or the cache returns an error, the error is propagated.
// If the cache Set fails after a successful invoke, the value is still
// returned (the Set error is swallowed since the primary operation succeeded).
func Exec[T any](ctx context.Context, config CacheConfig, c Cache, invoke Invoker[T]) (bool, T, error) {
	// Try cache first.
	found, val, err := GetContext[T](ctx, c, config.Key)
	if err != nil {
		var zero T
		return false, zero, err
	}
	if found {
		return true, val, nil
	}

	// Cache miss — invoke the function.
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
	// When config.Expires is zero, Set uses the cache's configured default TTL.
	_ = c.SetContext(ctx, config.Key, result, config.Expires)

	return true, result, nil
}
