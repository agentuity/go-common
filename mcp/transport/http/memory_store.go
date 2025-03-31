package http

import (
	"sync"
	"time"

	"github.com/agentuity/go-common/mcp/types"
)

type InMemorySessionStore struct {
	sessions map[string]*ClientSession
	mu       sync.RWMutex
}

func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		sessions: make(map[string]*ClientSession),
	}
}

func (store *InMemorySessionStore) Sessions() map[string]*ClientSession {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.sessions
}

func (store *InMemorySessionStore) GetOrCreateSession(sessionID string) *ClientSession {
	store.mu.Lock()
	defer store.mu.Unlock()

	session, ok := store.sessions[sessionID]
	if !ok {
		session = &ClientSession{
			ID:           sessionID,
			MessageQueue: make(chan *types.JSONRPCMessage, 100),
			Done:         make(chan struct{}),
			LastAccess:   time.Now(),
		}
		store.sessions[sessionID] = session
	} else {
		session.LastAccess = time.Now()
	}

	return session
}

func (store *InMemorySessionStore) CloseSession(sessionID string) {
	store.mu.Lock()
	defer store.mu.Unlock()

	if session, ok := store.sessions[sessionID]; ok {
		close(session.Done)
		close(session.MessageQueue)
		delete(store.sessions, sessionID)
	}
}

func (store *InMemorySessionStore) CloseAllSessions() {
	store.mu.Lock()
	defer store.mu.Unlock()

	for _, session := range store.sessions {
		close(session.Done)
		close(session.MessageQueue)
	}

	store.sessions = make(map[string]*ClientSession)
}
