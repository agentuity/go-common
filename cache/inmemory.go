package cache

import (
	"context"
	"sync"
	"time"
)

type inMemoryCache struct {
	ctx         context.Context
	cancel      context.CancelFunc
	cache       map[string]*value
	mutex       sync.Mutex
	waitGroup   sync.WaitGroup
	once        sync.Once
	expiryCheck time.Duration
}

var _ Cache = (*inMemoryCache)(nil)

func (c *inMemoryCache) Get(key string) (bool, any, error) {
	c.mutex.Lock()
	val, ok := c.cache[key]
	if ok {
		val.hits++
	}
	c.mutex.Unlock()
	if ok {
		if val.expires.Before(time.Now()) {
			c.mutex.Lock()
			delete(c.cache, key)
			c.mutex.Unlock()
			return false, nil, nil
		}
		return true, val.object, nil
	}
	return false, nil, nil
}

// Hits returns the number of times a key has been accessed.
func (c *inMemoryCache) Hits(key string) (bool, int) {
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

func (c *inMemoryCache) Set(key string, val any, expires time.Duration) error {
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

func (c *inMemoryCache) Expire(key string) (bool, error) {
	c.mutex.Lock()
	_, ok := c.cache[key]
	if ok {
		delete(c.cache, key)
	}
	c.mutex.Unlock()
	return ok, nil
}

func (c *inMemoryCache) Close() error {
	c.once.Do(func() {
		c.cancel()
		c.waitGroup.Wait()
	})
	return nil
}

func (c *inMemoryCache) run() {
	defer c.waitGroup.Done()
	interval := c.expiryCheck
	if interval <= 0 {
		interval = time.Minute
	}
	timer := time.NewTicker(interval)
	defer timer.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-timer.C:
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

// New returns a new Cache implementation
func NewInMemory(parent context.Context, expiryCheck time.Duration) Cache {
	ctx, cancel := context.WithCancel(parent)
	c := &inMemoryCache{
		ctx:         ctx,
		cancel:      cancel,
		cache:       make(map[string]*value),
		expiryCheck: expiryCheck,
	}
	if c.expiryCheck <= 0 {
		c.expiryCheck = time.Minute
	}
	c.waitGroup.Add(1)
	go c.run()
	return c
}
