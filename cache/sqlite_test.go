package cache

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSQLiteSimpleCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewSQLite(ctx, ":memory:", WithExpiryCheck(time.Second))
	assert.NoError(t, err)
	assert.NoError(t, c.Close())
}

func TestSQLiteSetGetCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewSQLite(ctx, ":memory:", WithExpiryCheck(time.Minute))
	assert.NoError(t, err)
	defer c.Close()

	// Miss on empty cache.
	found, val, err := c.Get("test")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)

	// Set and get raw.
	assert.NoError(t, c.Set("test", "value", time.Minute))
	found, val, err = c.Get("test")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.NotNil(t, val)

	// Get using generic helper.
	ok, str, err := Get[string](c, "test")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "value", str)
}

func TestSQLiteCacheExpiry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewSQLite(ctx, ":memory:", WithExpiryCheck(time.Minute))
	assert.NoError(t, err)
	defer c.Close()

	assert.NoError(t, c.Set("test", "value", 50*time.Millisecond))
	found, _, err := c.Get("test")
	assert.NoError(t, err)
	assert.True(t, found)

	time.Sleep(60 * time.Millisecond)
	found, val, err := c.Get("test")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)
}

func TestSQLiteCacheBackgroundExpiry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewSQLite(ctx, ":memory:", WithExpiryCheck(100*time.Millisecond))
	assert.NoError(t, err)
	defer c.Close()

	assert.NoError(t, c.Set("test", "value", 80*time.Millisecond))
	found, _, err := c.Get("test")
	assert.NoError(t, err)
	assert.True(t, found)

	// Wait for background cleanup to run.
	time.Sleep(250 * time.Millisecond)

	// The background goroutine should have deleted the expired entry.
	found, _, err = c.Get("test")
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestSQLiteCacheExpireMethod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewSQLite(ctx, ":memory:", WithExpiryCheck(time.Minute))
	assert.NoError(t, err)
	defer c.Close()

	assert.NoError(t, c.Set("test", "value", time.Minute))
	found, err := c.Expire("test")
	assert.NoError(t, err)
	assert.True(t, found)

	found, _, err = c.Get("test")
	assert.NoError(t, err)
	assert.False(t, found)

	// Expire a non-existent key.
	found, err = c.Expire("nonexistent")
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestSQLiteCacheHits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewSQLite(ctx, ":memory:", WithExpiryCheck(time.Minute))
	assert.NoError(t, err)
	defer c.Close()

	// No hits for missing key.
	ok, hits := c.Hits("test")
	assert.False(t, ok)
	assert.Equal(t, 0, hits)

	assert.NoError(t, c.Set("test", "value", time.Minute))

	// Hits starts at 0.
	ok, hits = c.Hits("test")
	assert.True(t, ok)
	assert.Equal(t, 0, hits)

	// Each Get increments hits.
	c.Get("test")
	c.Get("test")
	c.Get("test")
	ok, hits = c.Hits("test")
	assert.True(t, ok)
	assert.Equal(t, 3, hits)
}

func TestSQLiteInMemory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Empty string should default to :memory:.
	c, err := NewSQLite(ctx, "", WithExpiryCheck(time.Minute))
	assert.NoError(t, err)
	defer c.Close()

	assert.NoError(t, c.Set("key", 42, time.Minute))
	ok, val, err := Get[int](c, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 42, val)
}

func TestSQLiteFileBased(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	c, err := NewSQLite(ctx, dbPath, WithExpiryCheck(time.Minute))
	assert.NoError(t, err)
	defer c.Close()

	assert.NoError(t, c.Set("key", "file-based", time.Minute))
	ok, val, err := Get[string](c, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "file-based", val)
}

func TestSQLiteComplexTypes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewSQLite(ctx, ":memory:", WithExpiryCheck(time.Minute))
	assert.NoError(t, err)
	defer c.Close()

	// Struct.
	type Person struct {
		Name string `msgpack:"name"`
		Age  int    `msgpack:"age"`
	}
	p := Person{Name: "Alice", Age: 30}
	assert.NoError(t, c.Set("person", p, time.Minute))
	ok, gotP, err := Get[Person](c, "person")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, p, gotP)

	// Map.
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	assert.NoError(t, c.Set("map", m, time.Minute))
	ok, gotM, err := Get[map[string]int](c, "map")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, m, gotM)

	// Slice.
	s := []string{"hello", "world"}
	assert.NoError(t, c.Set("slice", s, time.Minute))
	ok, gotS, err := Get[[]string](c, "slice")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, s, gotS)

	// Nested struct.
	type Team struct {
		Name    string   `msgpack:"name"`
		Members []Person `msgpack:"members"`
	}
	team := Team{Name: "Engineering", Members: []Person{p, {Name: "Bob", Age: 25}}}
	assert.NoError(t, c.Set("team", team, time.Minute))
	ok, gotTeam, err := Get[Team](c, "team")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, team, gotTeam)
}

func TestSQLiteContextMethods(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewSQLite(ctx, ":memory:", WithExpiryCheck(time.Minute))
	assert.NoError(t, err)
	defer c.Close()

	// SetContext + GetContext round-trip.
	assert.NoError(t, c.SetContext(ctx, "key", "ctx-value", time.Minute))
	found, val, err := c.GetContext(ctx, "key")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.NotNil(t, val)

	// Typed retrieval via GetContext[T].
	ok, str, err := GetContext[string](ctx, c, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "ctx-value", str)

	// HitsContext after GetContext.
	ok, hits := c.HitsContext(ctx, "key")
	assert.True(t, ok)
	// Two GetContext calls above (raw + via GetContext[T]).
	assert.Equal(t, 2, hits)

	// ExpireContext removes entry.
	found2, err := c.ExpireContext(ctx, "key")
	assert.NoError(t, err)
	assert.True(t, found2)
	found, _, err = c.GetContext(ctx, "key")
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestSQLiteContextCancellation(t *testing.T) {
	bgCtx := context.Background()
	c, err := NewSQLite(bgCtx, ":memory:", WithExpiryCheck(time.Minute))
	assert.NoError(t, err)
	defer c.Close()

	// Create a cancelled context.
	cancelledCtx, cancel := context.WithCancel(bgCtx)
	cancel()

	// GetContext with cancelled context should return an error.
	_, _, err = c.GetContext(cancelledCtx, "key")
	assert.Error(t, err)

	// SetContext with cancelled context should return an error.
	err = c.SetContext(cancelledCtx, "key", "value", time.Minute)
	assert.Error(t, err)
}

func TestSQLiteOverwrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewSQLite(ctx, ":memory:", WithExpiryCheck(time.Minute))
	assert.NoError(t, err)
	defer c.Close()

	assert.NoError(t, c.Set("key", "first", time.Minute))
	ok, v, err := Get[string](c, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "first", v)

	// Get[string] called Get internally, so 1 hit.
	ok, hits := c.Hits("key")
	assert.True(t, ok)
	assert.Equal(t, 1, hits)

	// Overwrite resets hits and value.
	assert.NoError(t, c.Set("key", "second", time.Minute))
	ok, v, err = Get[string](c, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "second", v)

	// 1 hit from the Get[string] above (overwrite reset hits to 0).
	ok, hits = c.Hits("key")
	assert.True(t, ok)
	assert.Equal(t, 1, hits)
}
