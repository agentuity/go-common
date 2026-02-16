package cache

import "time"

type compositeCache struct {
	caches []Cache
}

var _ Cache = (*compositeCache)(nil)

// NewComposite returns a Cache that chains multiple caches together.
// Get checks caches in order and returns the first hit.
// Set writes to all caches.
// At least one cache must be provided; panics if empty.
func NewComposite(caches ...Cache) Cache {
	if len(caches) == 0 {
		panic("cache: NewComposite requires at least one cache")
	}
	return &compositeCache{caches: caches}
}

func (c *compositeCache) Get(key string) (bool, any, error) {
	for _, cache := range c.caches {
		found, val, err := cache.Get(key)
		if err != nil {
			return false, nil, err
		}
		if found {
			return true, val, nil
		}
	}
	return false, nil, nil
}

func (c *compositeCache) Set(key string, val any, expires time.Duration) error {
	for _, cache := range c.caches {
		if err := cache.Set(key, val, expires); err != nil {
			return err
		}
	}
	return nil
}

func (c *compositeCache) Hits(key string) (bool, int) {
	for _, cache := range c.caches {
		found, hits := cache.Hits(key)
		if found {
			return true, hits
		}
	}
	return false, 0
}

func (c *compositeCache) Expire(key string) (bool, error) {
	anyFound := false
	for _, cache := range c.caches {
		found, err := cache.Expire(key)
		if err != nil {
			return anyFound, err
		}
		if found {
			anyFound = true
		}
	}
	return anyFound, nil
}

func (c *compositeCache) Close() error {
	var firstErr error
	for _, cache := range c.caches {
		if err := cache.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
