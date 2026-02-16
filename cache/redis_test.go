package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return mr, client
}

func TestRedisSimpleCache(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()
	c := NewRedis(context.Background(), client, WithPrefix("test"))
	assert.NoError(t, c.Close())
}

func TestRedisSetGetCache(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()
	c := NewRedis(context.Background(), client, WithPrefix("test"))
	defer c.Close()

	// Miss on empty cache.
	found, val, err := c.Get("key")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)

	// Set and get raw.
	assert.NoError(t, c.Set("key", "value", time.Minute))
	found, val, err = c.Get("key")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.NotNil(t, val)

	// Get using generic helper.
	ok, str, err := Get[string](c, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "value", str)
}

func TestRedisCacheExpiry(t *testing.T) {
	mr, client := newTestRedis(t)
	defer client.Close()
	c := NewRedis(context.Background(), client, WithPrefix("test"))
	defer c.Close()

	assert.NoError(t, c.Set("key", "value", 2*time.Second))
	found, _, err := c.Get("key")
	assert.NoError(t, err)
	assert.True(t, found)

	// Use miniredis FastForward to simulate time passing.
	mr.FastForward(3 * time.Second)

	found, val, err := c.Get("key")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, val)
}

func TestRedisCacheExpireMethod(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()
	c := NewRedis(context.Background(), client, WithPrefix("test"))
	defer c.Close()

	assert.NoError(t, c.Set("key", "value", time.Minute))
	found, err := c.Expire("key")
	assert.NoError(t, err)
	assert.True(t, found)

	found, _, err = c.Get("key")
	assert.NoError(t, err)
	assert.False(t, found)

	// Expire a non-existent key.
	found, err = c.Expire("nonexistent")
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestRedisCacheHits(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()
	c := NewRedis(context.Background(), client, WithPrefix("test"))
	defer c.Close()

	// No hits for missing key.
	ok, hits := c.Hits("key")
	assert.False(t, ok)
	assert.Equal(t, 0, hits)

	assert.NoError(t, c.Set("key", "value", time.Minute))

	// Hits starts at 0.
	ok, hits = c.Hits("key")
	assert.True(t, ok)
	assert.Equal(t, 0, hits)

	// Each Get increments hits.
	c.Get("key")
	c.Get("key")
	c.Get("key")
	ok, hits = c.Hits("key")
	assert.True(t, ok)
	assert.Equal(t, 3, hits)
}

func TestRedisInMemory(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()
	c := NewRedis(context.Background(), client, WithPrefix("test"))
	defer c.Close()

	assert.NoError(t, c.Set("key", 42, time.Minute))
	ok, val, err := Get[int](c, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 42, val)
}

func TestRedisComplexTypes(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()
	c := NewRedis(context.Background(), client, WithPrefix("test"))
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

func TestRedisOverwrite(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()
	c := NewRedis(context.Background(), client, WithPrefix("test"))
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

func TestRedisPrefix(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()

	c1 := NewRedis(context.Background(), client, WithPrefix("ns1"))
	defer c1.Close()
	c2 := NewRedis(context.Background(), client, WithPrefix("ns2"))
	defer c2.Close()

	// Set same key in different namespaces.
	assert.NoError(t, c1.Set("key", "from-ns1", time.Minute))
	assert.NoError(t, c2.Set("key", "from-ns2", time.Minute))

	// Each namespace sees its own value.
	ok, v1, err := Get[string](c1, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "from-ns1", v1)

	ok, v2, err := Get[string](c2, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "from-ns2", v2)

	// Expire in one namespace doesn't affect the other.
	found, err := c1.Expire("key")
	assert.NoError(t, err)
	assert.True(t, found)

	found, _, err = c1.Get("key")
	assert.NoError(t, err)
	assert.False(t, found)

	ok, v2, err = Get[string](c2, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "from-ns2", v2)
}

func TestRedisNoPrefix(t *testing.T) {
	mr, client := newTestRedis(t)
	defer client.Close()
	c := NewRedis(context.Background(), client)
	defer c.Close()

	assert.NoError(t, c.Set("mykey", "myvalue", time.Minute))

	// Verify key stored without prefix by checking Redis directly.
	keys := mr.Keys()
	assert.Contains(t, keys, "mykey")

	ok, v, err := Get[string](c, "mykey")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "myvalue", v)
}

func TestRedisContextMethods(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()
	ctx := context.Background()
	c := NewRedis(ctx, client, WithPrefix("ctxtest"))
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

	// CloseContext is a no-op.
	assert.NoError(t, c.CloseContext(ctx))
}

func TestRedisContextCancellation(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()
	c := NewRedis(context.Background(), client, WithPrefix("canceltest"))
	defer c.Close()

	// Create a cancelled context.
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	// GetContext with cancelled context should return an error.
	_, _, err := c.GetContext(cancelledCtx, "key")
	assert.Error(t, err)

	// SetContext with cancelled context should return an error.
	err = c.SetContext(cancelledCtx, "key", "value", time.Minute)
	assert.Error(t, err)
}

func TestRedisComposite(t *testing.T) {
	_, client := newTestRedis(t)
	defer client.Close()

	ctx := context.Background()
	l1 := NewInMemory(ctx, WithExpiryCheck(time.Minute))
	l2 := NewRedis(ctx, client, WithPrefix("composite"))
	c := NewComposite(l1, l2)
	defer c.Close()

	// Set only in Redis (l2).
	assert.NoError(t, l2.Set("key", "redis-value", time.Minute))

	// l1 miss, l2 hit — composite finds it in Redis.
	found, _, err := c.Get("key")
	assert.NoError(t, err)
	assert.True(t, found)

	// Use generics to get typed value through composite.
	ok, val, err := Get[string](c, "key")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "redis-value", val)

	// Complete miss.
	found, v, err := c.Get("missing")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, v)
}
