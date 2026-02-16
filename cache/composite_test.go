package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCompositeSimple(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l1 := NewInMemory(ctx, time.Minute)
	l2 := NewInMemory(ctx, time.Minute)
	c := NewComposite(l1, l2)
	assert.NoError(t, c.Close())
}

func TestCompositePanicOnEmpty(t *testing.T) {
	assert.Panics(t, func() {
		NewComposite()
	})
}

func TestCompositeGetOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1 := NewInMemory(ctx, time.Minute)
	l2 := NewInMemory(ctx, time.Minute)
	c := NewComposite(l1, l2)
	defer c.Close()

	// Set different values in each layer directly.
	l1.Set("key", "from-l1", time.Minute)
	l2.Set("key", "from-l2", time.Minute)

	// Composite should return from the first cache (l1).
	found, val, err := c.Get("key")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "from-l1", val)
}

func TestCompositeSetAll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1 := NewInMemory(ctx, time.Minute)
	l2 := NewInMemory(ctx, time.Minute)
	c := NewComposite(l1, l2)
	defer c.Close()

	// Set via composite writes to all.
	assert.NoError(t, c.Set("key", "shared", time.Minute))

	// Both layers should have it.
	found, val, err := l1.Get("key")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "shared", val)

	found, val, err = l2.Get("key")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "shared", val)
}

func TestCompositeExpireAll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1 := NewInMemory(ctx, time.Minute)
	l2 := NewInMemory(ctx, time.Minute)
	c := NewComposite(l1, l2)
	defer c.Close()

	assert.NoError(t, c.Set("key", "value", time.Minute))

	found, err := c.Expire("key")
	assert.NoError(t, err)
	assert.True(t, found)

	// Both layers should be empty.
	found, _, err = l1.Get("key")
	assert.NoError(t, err)
	assert.False(t, found)

	found, _, err = l2.Get("key")
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestCompositeExpireNonexistent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1 := NewInMemory(ctx, time.Minute)
	c := NewComposite(l1)
	defer c.Close()

	found, err := c.Expire("nope")
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestCompositeHits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1 := NewInMemory(ctx, time.Minute)
	l2 := NewInMemory(ctx, time.Minute)
	c := NewComposite(l1, l2)
	defer c.Close()

	assert.NoError(t, c.Set("key", "value", time.Minute))

	// Access through composite a few times.
	c.Get("key")
	c.Get("key")

	// Hits should return from the first cache that has the key.
	ok, hits := c.Hits("key")
	assert.True(t, ok)
	assert.Equal(t, 2, hits)
}

func TestCompositeFallthrough(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1 := NewInMemory(ctx, time.Minute)
	l2 := NewInMemory(ctx, time.Minute)
	c := NewComposite(l1, l2)
	defer c.Close()

	// Set only in l2.
	l2.Set("key", "only-in-l2", time.Minute)

	// l1 miss, l2 hit.
	found, val, err := c.Get("key")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "only-in-l2", val)

	// Complete miss.
	found, val, err = c.Get("missing")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)
}

func TestCompositeMixedCacheTypes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1 := NewInMemory(ctx, time.Minute)
	l2, err := NewSQLite(ctx, ":memory:", time.Minute)
	assert.NoError(t, err)
	c := NewComposite(l1, l2)
	defer c.Close()

	// Set only in SQLite (l2).
	l2.Set("key", "sqlite-value", time.Minute)

	// Composite finds it in l2 (returns []byte from SQLite).
	found, _, err := c.Get("key")
	assert.NoError(t, err)
	assert.True(t, found)

	// Use generics to get typed value through composite.
	ok, val, err := Get[string](c, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "sqlite-value", val)
}

func TestCompositeSingleCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1 := NewInMemory(ctx, time.Minute)
	c := NewComposite(l1)
	defer c.Close()

	assert.NoError(t, c.Set("key", "value", time.Minute))
	found, val, err := c.Get("key")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value", val)
}
