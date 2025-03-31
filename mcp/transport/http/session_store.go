package http

import (
	"time"

	"github.com/agentuity/go-common/mcp/types"
)

type SessionStore interface {
	GetOrCreateSession(sessionID string) *ClientSession

	CloseSession(sessionID string)

	CloseAllSessions()

	Sessions() map[string]*ClientSession
}

type ClientSession struct {
	ID           string
	MessageQueue chan *types.JSONRPCMessage
	Done         chan struct{}
	LastAccess   time.Time
}
