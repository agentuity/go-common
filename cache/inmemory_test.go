package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSimpleCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cache := NewInMemory(ctx, time.Second)
	cache.Close()
	cancel()
}

func TestSetGetCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cache := NewInMemory(ctx, time.Minute)
	found, val, err := cache.Get("test")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)
	assert.NoError(t, cache.Set("test", "value", time.Millisecond*10))
	found, val, err = cache.Get("test")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value", val)
	ok, hits := cache.Hits("test")
	assert.True(t, ok)
	assert.Equal(t, 1, hits)
	time.Sleep(time.Millisecond * 11)
	found, val, err = cache.Get("test")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)
	ok, hits = cache.Hits("test")
	assert.False(t, ok)
	assert.Equal(t, 0, hits)
	cache.Close()
	cancel()
}

func TestCacheBackgroundExpire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cache := NewInMemory(ctx, time.Millisecond*100)
	found, val, err := cache.Get("test")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)
	assert.NoError(t, cache.Set("test", "value", 90*time.Millisecond))
	found, val, err = cache.Get("test")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value", val)
	time.Sleep(time.Millisecond * 200)
	c := cache.(*inMemoryCache)
	c.mutex.Lock()
	defer c.mutex.Unlock()
	assert.Empty(t, c.cache)
	cache.Close()
	cancel()
}

func TestCacheExpire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cache := NewInMemory(ctx, time.Millisecond*100)
	found, val, err := cache.Get("test")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)
	assert.NoError(t, cache.Set("test", "value", 90*time.Millisecond))
	found, val, err = cache.Get("test")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value", val)
	cache.Expire("test")
	c := cache.(*inMemoryCache)
	c.mutex.Lock()
	defer c.mutex.Unlock()
	assert.Empty(t, c.cache)
	cache.Close()
	cancel()
}
