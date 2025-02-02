package cache

import (
	"time"
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
