package http

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/agentuity/go-common/mcp/types"
	"github.com/redis/go-redis/v9"
)

const (
	defaultSessionExpiration = 24 * time.Hour
	sessionKeyPrefix         = "mcp:session:"
	defaultCleanupInterval   = 30 * time.Minute
)

type RedisSessionStore struct {
	rdb             *redis.Client
	ctx             context.Context
	cancel          context.CancelFunc
	sessionExpiry   time.Duration
	cleanupInterval time.Duration
	localSessions   map[string]*ClientSession
	mu              sync.RWMutex
}

type RedisSessionStoreOption func(*RedisSessionStore)

func WithSessionExpiration(duration time.Duration) RedisSessionStoreOption {
	return func(store *RedisSessionStore) {
		store.sessionExpiry = duration
	}
}

func WithCleanupInterval(duration time.Duration) RedisSessionStoreOption {
	return func(store *RedisSessionStore) {
		store.cleanupInterval = duration
	}
}

func NewRedisSessionStore(ctx context.Context, rdb *redis.Client, options ...RedisSessionStoreOption) *RedisSessionStore {
	cleanupCtx, cancel := context.WithCancel(ctx)

	store := &RedisSessionStore{
		rdb:             rdb,
		ctx:             ctx,
		cancel:          cancel,
		sessionExpiry:   defaultSessionExpiration,
		cleanupInterval: defaultCleanupInterval,
		localSessions:   make(map[string]*ClientSession),
	}

	for _, option := range options {
		option(store)
	}

	go store.periodicCleanup(cleanupCtx)

	return store
}

func (store *RedisSessionStore) periodicCleanup(ctx context.Context) {
	ticker := time.NewTicker(store.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			store.cleanupExpiredSessions()
		}
	}
}

func (store *RedisSessionStore) cleanupExpiredSessions() {
	pattern := fmt.Sprintf("%s*", sessionKeyPrefix)
	var cursor uint64
	var keys []string

	for {
		var batch []string
		var err error
		batch, cursor, err = store.rdb.Scan(store.ctx, cursor, pattern, 100).Result()
		if err != nil {
			return
		}

		keys = append(keys, batch...)
		if cursor == 0 {
			break
		}
	}

	for _, key := range keys {
		sessionID := key[len(sessionKeyPrefix):]
		exists, err := store.rdb.Exists(store.ctx, key).Result()
		if err != nil || exists == 0 {
			store.CloseSession(sessionID)
		}
	}
}

func (store *RedisSessionStore) Sessions() map[string]*ClientSession {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.localSessions
}

func (store *RedisSessionStore) GetOrCreateSession(sessionID string) *ClientSession {
	store.mu.Lock()
	defer store.mu.Unlock()

	session, ok := store.localSessions[sessionID]
	if ok {
		session.LastAccess = time.Now()
		store.updateSessionInRedis(sessionID, session)
		return session
	}

	key := fmt.Sprintf("%s%s", sessionKeyPrefix, sessionID)
	data, err := store.rdb.Get(store.ctx, key).Bytes()

	if err == nil {
		var sessionData struct {
			LastAccess time.Time `json:"lastAccess"`
		}

		if err := json.Unmarshal(data, &sessionData); err == nil {
			session = &ClientSession{
				ID:           sessionID,
				MessageQueue: make(chan *types.JSONRPCMessage, 100),
				Done:         make(chan struct{}),
				LastAccess:   sessionData.LastAccess,
			}
			store.localSessions[sessionID] = session
			return session
		}
	}

	session = &ClientSession{
		ID:           sessionID,
		MessageQueue: make(chan *types.JSONRPCMessage, 100),
		Done:         make(chan struct{}),
		LastAccess:   time.Now(),
	}

	store.localSessions[sessionID] = session
	store.updateSessionInRedis(sessionID, session)

	return session
}

func (store *RedisSessionStore) updateSessionInRedis(sessionID string, session *ClientSession) {
	sessionData := struct {
		LastAccess time.Time `json:"lastAccess"`
	}{
		LastAccess: session.LastAccess,
	}

	data, err := json.Marshal(sessionData)
	if err != nil {
		return
	}

	key := fmt.Sprintf("%s%s", sessionKeyPrefix, sessionID)
	store.rdb.Set(store.ctx, key, data, store.sessionExpiry)
}

func (store *RedisSessionStore) CloseSession(sessionID string) {
	store.mu.Lock()
	defer store.mu.Unlock()

	if session, ok := store.localSessions[sessionID]; ok {
		close(session.Done)
		close(session.MessageQueue)
		delete(store.localSessions, sessionID)
	}

	key := fmt.Sprintf("%s%s", sessionKeyPrefix, sessionID)
	store.rdb.Del(store.ctx, key)
}

func (store *RedisSessionStore) CloseAllSessions() {
	store.mu.Lock()
	defer store.mu.Unlock()

	for sessionID, session := range store.localSessions {
		close(session.Done)
		close(session.MessageQueue)

		key := fmt.Sprintf("%s%s", sessionKeyPrefix, sessionID)
		store.rdb.Del(store.ctx, key)
	}

	store.localSessions = make(map[string]*ClientSession)
}

func (store *RedisSessionStore) Close() error {
	store.cancel()
	store.CloseAllSessions()
	return nil
}
