package cache

import (
	"context"
	"sync"
	"time"
)

type inMemoryCache struct {
	ctx       context.Context
	cancel    context.CancelFunc
	cache     map[string]*value
	mutex     sync.Mutex
	waitGroup sync.WaitGroup
	once      sync.Once
	cfg       config
}

var _ Cache = (*inMemoryCache)(nil)

func (c *inMemoryCache) GetContext(_ context.Context, key string) (bool, any, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	val, ok := c.cache[key]
	if !ok {
		return false, nil, nil
	}
	if val.expires.Before(time.Now()) {
		delete(c.cache, key)
		return false, nil, nil
	}
	val.hits++
	return true, val.object, nil
}

func (c *inMemoryCache) Get(key string) (bool, any, error) {
	return c.GetContext(c.ctx, key)
}

func (c *inMemoryCache) HitsContext(_ context.Context, key string) (bool, int) {
	c.mutex.Lock()
	var val int
	var found bool
	if v, ok := c.cache[key]; ok {
		val = v.hits
		found = true
	}
	c.mutex.Unlock()
	return found, val
}

func (c *inMemoryCache) Hits(key string) (bool, int) {
	return c.HitsContext(c.ctx, key)
}

func (c *inMemoryCache) SetContext(_ context.Context, key string, val any, expires time.Duration) error {
	if expires <= 0 {
		expires = c.cfg.defaultExpires
	}
	c.mutex.Lock()
	if v, ok := c.cache[key]; ok {
		v.hits = 0
		v.expires = time.Now().Add(expires)
		v.object = val
	} else {
		c.cache[key] = &value{val, time.Now().Add(expires), 0}
	}
	c.mutex.Unlock()
	return nil
}

func (c *inMemoryCache) Set(key string, val any, expires time.Duration) error {
	return c.SetContext(c.ctx, key, val, expires)
}

func (c *inMemoryCache) ExpireContext(_ context.Context, key string) (bool, error) {
	c.mutex.Lock()
	_, ok := c.cache[key]
	if ok {
		delete(c.cache, key)
	}
	c.mutex.Unlock()
	return ok, nil
}

func (c *inMemoryCache) Expire(key string) (bool, error) {
	return c.ExpireContext(c.ctx, key)
}

func (c *inMemoryCache) CloseContext(_ context.Context) error {
	c.once.Do(func() {
		c.cancel()
		c.waitGroup.Wait()
	})
	return nil
}

func (c *inMemoryCache) Close() error {
	return c.CloseContext(c.ctx)
}

func (c *inMemoryCache) run() {
	defer c.waitGroup.Done()
	ticker := time.NewTicker(c.cfg.expiryCheck)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			c.mutex.Lock()
			for key, val := range c.cache {
				if val.expires.Before(now) {
					delete(c.cache, key)
				}
			}
			c.mutex.Unlock()
		}
	}
}

// NewInMemory returns a new in-memory Cache implementation.
func NewInMemory(parent context.Context, opts ...Option) Cache {
	cfg := applyOptions(opts)
	ctx, cancel := context.WithCancel(parent)
	c := &inMemoryCache{
		ctx:    ctx,
		cancel: cancel,
		cache:  make(map[string]*value),
		cfg:    cfg,
	}
	c.waitGroup.Add(1)
	go c.run()
	return c
}
