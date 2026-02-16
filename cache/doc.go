// Package cache provides a unified caching interface with multiple backend
// implementations and type-safe generic helpers.
//
// # Cache Interface
//
// The [Cache] interface defines ten operations: five context-aware methods
// ([Cache.GetContext], [Cache.SetContext], [Cache.HitsContext],
// [Cache.ExpireContext], [Cache.CloseContext]) and their deprecated
// non-context counterparts ([Cache.Get], [Cache.Set], [Cache.Hits],
// [Cache.Expire], [Cache.Close]). The context-aware variants are the
// preferred API — they accept a [context.Context] that controls cancellation
// and timeout for I/O-backed implementations. All implementations satisfy
// this interface, so backends can be swapped without changing application
// code.
//
// The interface uses [any] for values rather than generics because Go does
// not allow generic methods on interfaces. Type safety is provided by the
// package-level generic functions [GetContext] and [Exec] described below.
//
// # Context-Aware Methods
//
// Every operation has a context-aware variant (e.g. [Cache.GetContext] vs
// [Cache.Get]). The context-aware methods are the real implementations; the
// old methods are thin wrappers that delegate with a stored or background
// context for backward compatibility. New code should always use the
// context-aware variants:
//
//	found, val, err := c.GetContext(ctx, "user:123")
//	err = c.SetContext(ctx, "user:123", user, 10*time.Minute)
//	found, err = c.ExpireContext(ctx, "user:123")
//
// For in-memory caches the context is accepted for interface consistency but
// is not actively used (operations are instant). For I/O-backed caches
// (SQLite, Redis) the context controls cancellation and interacts with the
// per-operation query timeout.
//
// # Implementations
//
// Four implementations are provided, each with different tradeoffs:
//
//   - [NewInMemory] — In-process map guarded by a mutex. Fastest option with
//     zero serialization overhead. Values are stored as-is (no copying), so
//     mutations to stored pointers are visible through the cache. Expired
//     entries are cleaned up by a background goroutine at a configurable
//     interval. Lost on process restart.
//
//   - [NewSQLite] — Backed by a SQLite database using [modernc.org/sqlite]
//     (pure Go, no CGO). Values are serialized to msgpack and stored as BLOBs.
//     Supports both file-backed (persistent across restarts) and ":memory:"
//     modes. WAL mode is enabled for concurrent read performance. Each
//     operation uses a per-query timeout ([DefaultQueryTimeout]) to prevent
//     hangs on slow storage.
//
//   - [NewRedis] — Backed by Redis using [github.com/redis/go-redis/v9].
//     Values are serialized to msgpack and stored in Redis hashes (fields "v"
//     for value, "h" for hit count). Expiry uses native Redis TTL — no
//     background goroutine is needed. An optional key prefix supports
//     namespacing multiple caches on the same Redis instance. The caller owns
//     the [redis.Client] lifecycle; [Cache.Close] is a no-op. Each operation
//     uses a per-query timeout ([DefaultQueryTimeout]).
//
//   - [NewComposite] — Chains multiple [Cache] implementations in order.
//     [Cache.GetContext] returns the first hit (checked left to right).
//     [Cache.SetContext] writes to all caches. [Cache.ExpireContext] removes
//     from all caches. This enables multi-tier topologies such as in-memory
//     L1 backed by Redis L2.
//
// # Generic Helpers
//
// [GetContext] is a generic function that wraps [Cache.GetContext] with type
// safety:
//
//	found, user, err := cache.GetContext[User](ctx, c, "user:123")
//
// For in-memory caches, [GetContext] performs a direct type assertion (zero
// cost). For serialized backends (SQLite, Redis), it deserializes the stored
// []byte via msgpack. This means [GetContext] works transparently regardless
// of which backend produced the value.
//
// The deprecated [Get] function is equivalent but uses [context.Background]:
//
//	found, user, err := cache.Get[User](c, "user:123")  // Deprecated
//
// [Exec] is a cache-aside (read-through) helper that combines lookup and
// population in one call. It uses context-aware methods internally:
//
//	found, user, err := cache.Exec(ctx, cache.CacheConfig{Key: "user:123"}, c,
//	    func(ctx context.Context) (User, bool, error) {
//	        user, err := queries.GetUser(ctx, id)
//	        if errors.Is(err, sql.ErrNoRows) {
//	            return User{}, false, nil   // not found — won't be cached
//	        }
//	        return user, true, err          // found — will be cached
//	    },
//	)
//
// The [Invoker] function returns (value, found, error). The found bool
// distinguishes "not found" from "found a zero value", preventing the cache
// from storing absent records. When found is false, nothing is cached and
// subsequent calls will invoke again — useful for cases like sql.ErrNoRows
// where caching a zero value would serve stale "empty" results.
//
// # Error Handling
//
// Cache read errors are always propagated. If [Cache.GetContext] returns an
// error (e.g., SQLite I/O failure, Redis timeout, context cancellation),
// [Exec] returns that error immediately without calling the invoker. This
// prevents cache-failure stampedes where every request bypasses cache and
// overwhelms the backing store.
//
// Cache write errors in [Exec] are swallowed — if the invoker succeeds but
// [Cache.SetContext] fails, the value is still returned to the caller. The
// primary operation (producing the value) succeeded; failing to cache it is
// a degradation, not a failure.
//
// # Serialization
//
// The SQLite and Redis backends serialize values using msgpack
// ([github.com/vmihailenco/msgpack/v5]). Most Go types work out of the box:
// primitives, structs (exported fields), maps, slices, pointers, and types
// implementing msgpack.CustomEncoder/CustomDecoder.
//
// Types that cannot be serialized include functions, channels, and
// complex numbers. Attempting to store these in a serialized backend will
// cause [Cache.SetContext] to return a marshal error. The in-memory backend
// stores values as-is and has no serialization constraints.
//
// For struct fields to survive serialization, they must be exported. Use
// msgpack struct tags for field name control:
//
//	type User struct {
//	    Name  string `msgpack:"name"`
//	    Email string `msgpack:"email"`
//	}
//
// # Timeouts
//
// The SQLite and Redis backends apply a per-operation timeout
// ([DefaultQueryTimeout], 5 seconds) to every I/O operation. This prevents
// indefinite hangs on slow or unresponsive storage. The timeout is derived
// from the context passed to each context-aware method, so cancelling the
// caller's context also cancels in-flight operations. The deprecated
// non-context methods derive the timeout from the parent context passed to
// the constructor.
//
// # Choosing a Backend
//
// Use [NewInMemory] when speed is paramount and persistence is not needed.
// It is the fastest option and imposes no serialization constraints, but data
// is lost on process restart and is not shared across processes.
//
// Use [NewSQLite] when you need persistence across restarts without external
// infrastructure. File-backed SQLite survives process restarts and is a good
// fit for CLI tools, desktop apps, or single-node services.
//
// Use [NewRedis] when the cache must be shared across multiple processes or
// nodes. Redis provides sub-millisecond access and native TTL management, but
// requires a running Redis instance.
//
// Use [NewComposite] to combine backends into a multi-tier cache. A common
// pattern is in-memory L1 for hot data backed by Redis L2 for shared state:
//
//	c := cache.NewComposite(
//	    cache.NewInMemory(ctx, cache.WithExpires(time.Minute)),
//	    cache.NewRedis(ctx, redisClient, cache.WithPrefix("myapp")),
//	)
//
// # Configuration
//
// All constructors accept functional options to override defaults:
//
//	c := cache.NewSQLite(ctx, "/tmp/cache.db",
//	    cache.WithExpires(10*time.Minute),      // default TTL for cached values
//	    cache.WithQueryTimeout(3*time.Second),   // per-operation I/O timeout
//	    cache.WithExpiryCheck(30*time.Second),   // background cleanup interval
//	)
//
// Available options:
//
//   - [WithExpires] — Default TTL used by [Cache.SetContext] when expires <= 0.
//     Defaults to [DefaultExpires] (5 minutes).
//
//   - [WithQueryTimeout] — Per-operation timeout for I/O backends (SQLite,
//     Redis). Defaults to [DefaultQueryTimeout] (5 seconds).
//
//   - [WithExpiryCheck] — Background cleanup interval for expired entries.
//     Applies to InMemory and SQLite. Defaults to 1 minute.
//
//   - [WithPrefix] — Key prefix for namespacing. Applies to Redis.
//     Defaults to empty (no prefix).
//
// When [Cache.SetContext] is called with expires <= 0, the cache uses its
// configured default TTL ([WithExpires]) rather than creating an
// already-expired entry. This interacts cleanly with [Exec]: when
// CacheConfig.Expires is zero, SetContext receives zero, and the cache
// applies its own default.
package cache
