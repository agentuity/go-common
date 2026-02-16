package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestExecCacheMiss(t *testing.T) {
	ctx := context.Background()
	c := NewInMemory(ctx, time.Minute)
	defer c.Close()

	invoked := false
	found, val, err := Exec(ctx, CacheConfig{Key: "key", Expires: time.Minute}, c, func(ctx context.Context) (string, bool, error) {
		invoked = true
		return "fresh-value", true, nil
	})
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "fresh-value", val)
	assert.True(t, invoked)

	// Value should now be cached.
	cachedFound, cached, err := Get[string](c, "key")
	assert.NoError(t, err)
	assert.True(t, cachedFound)
	assert.Equal(t, "fresh-value", cached)
}

func TestExecCacheHit(t *testing.T) {
	ctx := context.Background()
	c := NewInMemory(ctx, time.Minute)
	defer c.Close()

	// Pre-populate.
	c.Set("key", "cached-value", time.Minute)

	invoked := false
	found, val, err := Exec(ctx, CacheConfig{Key: "key"}, c, func(ctx context.Context) (string, bool, error) {
		invoked = true
		return "fresh-value", true, nil
	})
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "cached-value", val)
	assert.False(t, invoked)
}

func TestExecInvokerError(t *testing.T) {
	ctx := context.Background()
	c := NewInMemory(ctx, time.Minute)
	defer c.Close()

	expectedErr := fmt.Errorf("invoke failed")
	found, val, err := Exec(ctx, CacheConfig{Key: "key", Expires: time.Minute}, c, func(ctx context.Context) (string, bool, error) {
		return "", false, expectedErr
	})
	assert.ErrorIs(t, err, expectedErr)
	assert.False(t, found)
	assert.Equal(t, "", val)

	// Nothing should be cached.
	ok, _, getErr := c.Get("key")
	assert.NoError(t, getErr)
	assert.False(t, ok)
}

func TestExecDefaultExpires(t *testing.T) {
	ctx := context.Background()
	c := NewInMemory(ctx, time.Minute)
	defer c.Close()

	// Expires is zero — should use DefaultExpires and still cache the value.
	found, val, err := Exec(ctx, CacheConfig{Key: "key"}, c, func(ctx context.Context) (int, bool, error) {
		return 42, true, nil
	})
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 42, val)

	// Value should be cached with DefaultExpires.
	cachedFound, cached, err := Get[int](c, "key")
	assert.NoError(t, err)
	assert.True(t, cachedFound)
	assert.Equal(t, 42, cached)

	// Now test with a very short custom Expires to verify expiration works.
	c2 := NewInMemory(ctx, time.Millisecond*50)
	defer c2.Close()

	found, val, err = Exec(ctx, CacheConfig{Key: "short", Expires: 20 * time.Millisecond}, c2, func(ctx context.Context) (int, bool, error) {
		return 99, true, nil
	})
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 99, val)

	// Should be cached immediately.
	cachedFound, cached, err = Get[int](c2, "short")
	assert.NoError(t, err)
	assert.True(t, cachedFound)
	assert.Equal(t, 99, cached)

	// Wait for expiry.
	time.Sleep(30 * time.Millisecond)
	cachedFound, _, err = Get[int](c2, "short")
	assert.NoError(t, err)
	assert.False(t, cachedFound)
}

func TestExecCustomExpires(t *testing.T) {
	ctx := context.Background()
	c := NewInMemory(ctx, time.Millisecond*50)
	defer c.Close()

	found, val, err := Exec(ctx, CacheConfig{Key: "key", Expires: 20 * time.Millisecond}, c, func(ctx context.Context) (string, bool, error) {
		return "ephemeral", true, nil
	})
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "ephemeral", val)

	// Cached right after.
	cachedFound, cached, err := Get[string](c, "key")
	assert.NoError(t, err)
	assert.True(t, cachedFound)
	assert.Equal(t, "ephemeral", cached)

	// Expires after the custom duration.
	time.Sleep(30 * time.Millisecond)
	cachedFound, _, err = Get[string](c, "key")
	assert.NoError(t, err)
	assert.False(t, cachedFound)
}

func TestExecWithSQLite(t *testing.T) {
	ctx := context.Background()
	c, err := NewSQLite(ctx, ":memory:", time.Minute)
	assert.NoError(t, err)
	defer c.Close()

	type Item struct {
		Name  string `msgpack:"name"`
		Count int    `msgpack:"count"`
	}

	expected := Item{Name: "widget", Count: 7}
	found, val, execErr := Exec(ctx, CacheConfig{Key: "item", Expires: time.Minute}, c, func(ctx context.Context) (Item, bool, error) {
		return expected, true, nil
	})
	assert.NoError(t, execErr)
	assert.True(t, found)
	assert.Equal(t, expected, val)

	// Second call should be a cache hit (deserialized via msgpack).
	invoked := false
	found, val, execErr = Exec(ctx, CacheConfig{Key: "item", Expires: time.Minute}, c, func(ctx context.Context) (Item, bool, error) {
		invoked = true
		return Item{}, true, nil
	})
	assert.NoError(t, execErr)
	assert.True(t, found)
	assert.Equal(t, expected, val)
	assert.False(t, invoked)
}

func TestExecInvokerCalledOnce(t *testing.T) {
	ctx := context.Background()
	c := NewInMemory(ctx, time.Minute)
	defer c.Close()

	callCount := 0
	invoker := func(ctx context.Context) (string, bool, error) {
		callCount++
		return "result", true, nil
	}

	cfg := CacheConfig{Key: "once", Expires: time.Minute}

	// First call — miss — invoker called, found=true because invoker returned ok.
	found, val, err := Exec(ctx, cfg, c, invoker)
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "result", val)
	assert.Equal(t, 1, callCount)

	// Second call — hit — invoker NOT called.
	found, val, err = Exec(ctx, cfg, c, invoker)
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "result", val)
	assert.Equal(t, 1, callCount)
}

func TestExecInvokerNotFound(t *testing.T) {
	ctx := context.Background()
	c := NewInMemory(ctx, time.Minute)
	defer c.Close()

	// Invoker returns not-found (no error).
	found, val, err := Exec(ctx, CacheConfig{Key: "key"}, c, func(ctx context.Context) (string, bool, error) {
		return "", false, nil
	})
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, "", val)

	// Nothing should be cached.
	ok, _, cacheErr := c.Get("key")
	assert.NoError(t, cacheErr)
	assert.False(t, ok)
}

func TestExecCacheReadError(t *testing.T) {
	ctx := context.Background()
	c := &errorCache{err: fmt.Errorf("disk I/O error")}

	invoked := false
	found, val, err := Exec(ctx, CacheConfig{Key: "key"}, c, func(ctx context.Context) (string, bool, error) {
		invoked = true
		return "value", true, nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disk I/O error")
	assert.False(t, found)
	assert.Equal(t, "", val)
	assert.False(t, invoked, "invoker should not be called when cache returns an error")
}

// errorCache is a test double that always returns an error from Get.
type errorCache struct {
	err error
}

func (e *errorCache) Get(string) (bool, any, error)        { return false, nil, e.err }
func (e *errorCache) Set(string, any, time.Duration) error { return e.err }
func (e *errorCache) Hits(string) (bool, int)              { return false, 0 }
func (e *errorCache) Expire(string) (bool, error)          { return false, e.err }
func (e *errorCache) Close() error                         { return nil }

func TestExecNotFoundThenFound(t *testing.T) {
	ctx := context.Background()
	c := NewInMemory(ctx, time.Minute)
	defer c.Close()

	callCount := 0

	invoker := func(ctx context.Context) (string, bool, error) {
		callCount++
		if callCount == 1 {
			return "", false, nil // first call: not found
		}
		return "appeared", true, nil // second call: found
	}

	// First call — not found, not cached.
	found, _, err := Exec(ctx, CacheConfig{Key: "key"}, c, invoker)
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, 1, callCount)

	// Second call — invoker finds it now, gets cached.
	found, val, err := Exec(ctx, CacheConfig{Key: "key"}, c, invoker)
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "appeared", val)
	assert.Equal(t, 2, callCount)

	// Third call — served from cache, invoker not called.
	found, val, err = Exec(ctx, CacheConfig{Key: "key"}, c, invoker)
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "appeared", val)
	assert.Equal(t, 2, callCount) // still 2, not called again
}
