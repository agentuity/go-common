package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/agentuity/go-common/logger"
	"github.com/agentuity/go-common/mcp/transport"
	"github.com/agentuity/go-common/mcp/types"
)

const (
	DefaultSessionHeaderName = "Mcp-Session-Id"
	DefaultSSERetryInterval  = 3000 // milliseconds
	DefaultUserAgent         = "Agentuity-MCP-Server/1.0"
)

type HTTPTransport struct {
	server         *http.Server
	mux            *http.ServeMux
	sessionStore   SessionStore
	logger         logger.Logger
	messageHandler transport.MessageHandler
	errorHandler   transport.ErrorHandler
	closeHandler   transport.CloseHandler
	path           string
	sessionHeader  string
	sseRetryMs     int
	userAgent      string
	mu             sync.Mutex
	closed         bool
	closeOnce      sync.Once
}

type HTTPTransportOption func(*HTTPTransport)

func WithSessionStore(sessionStore SessionStore) HTTPTransportOption {
	return func(t *HTTPTransport) {
		t.sessionStore = sessionStore
	}
}

func WithSessionHeader(headerName string) HTTPTransportOption {
	return func(t *HTTPTransport) {
		t.sessionHeader = headerName
	}
}

func WithSSERetryInterval(retryMs int) HTTPTransportOption {
	return func(t *HTTPTransport) {
		t.sseRetryMs = retryMs
	}
}

func WithUserAgent(userAgent string) HTTPTransportOption {
	return func(t *HTTPTransport) {
		t.userAgent = userAgent
	}
}

func WithServeMux(mux *http.ServeMux) HTTPTransportOption {
	return func(t *HTTPTransport) {
		t.mux = mux
	}
}

func NewHTTPTransport(addr string, path string, logger logger.Logger, options ...HTTPTransportOption) *HTTPTransport {
	transport := &HTTPTransport{
		sessionStore:  NewInMemorySessionStore(),
		logger:        logger,
		path:          path,
		sessionHeader: DefaultSessionHeaderName,
		sseRetryMs:    DefaultSSERetryInterval,
		userAgent:     DefaultUserAgent,
	}

	for _, option := range options {
		option(transport)
	}

	mux := http.NewServeMux()
	if transport.mux == nil {
		transport.mux = mux
	}

	transport.mux.HandleFunc(path, transport.handleStreamableHTTP)

	transport.server = &http.Server{
		Addr:    addr,
		Handler: transport.mux,
	}

	return transport
}

func (t *HTTPTransport) Start(ctx context.Context) error {
	go func() {
		if err := t.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if t.errorHandler != nil {
				t.errorHandler(err)
			}
		}
	}()

	go func() {
		<-ctx.Done()
		t.Close()
	}()

	return nil
}

func (t *HTTPTransport) handleStreamableHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(t.sessionHeader)
	if sessionID == "" {
		http.Error(w, "Missing session ID", http.StatusBadRequest)
		return
	}

	session := t.sessionStore.GetOrCreateSession(sessionID)

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("User-Agent", t.userAgent)
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, "retry: %d\n\n", t.sseRetryMs)
		flusher.Flush()

		for {
			select {
			case msg, ok := <-session.MessageQueue:
				if !ok {
					return
				}

				data, err := json.Marshal(msg)
				if err != nil {
					continue
				}

				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()

			case <-session.Done:
				return

			case <-r.Context().Done():
				return
			}
		}

	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}

		var message types.JSONRPCMessage
		if err := json.Unmarshal(body, &message); err != nil {
			http.Error(w, "Error parsing JSON-RPC message", http.StatusBadRequest)
			return
		}

		if t.messageHandler != nil {
			t.messageHandler(&message)
		}

		w.WriteHeader(http.StatusAccepted)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (t *HTTPTransport) Send(ctx context.Context, message *types.JSONRPCMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return io.ErrClosedPipe
	}

	sessions := t.sessionStore.Sessions()

	for _, session := range sessions {
		select {
		case session.MessageQueue <- message:
		default:
			t.logger.Warn("Message queue full for session %s", session.ID)
		}
	}

	return nil
}

func (t *HTTPTransport) Close() error {
	var err error

	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		err = t.server.Shutdown(ctx)
		t.sessionStore.CloseAllSessions()

		if t.closeHandler != nil {
			t.closeHandler()
		}
	})

	return err
}

func (t *HTTPTransport) SetMessageHandler(handler transport.MessageHandler) {
	t.messageHandler = handler
}

func (t *HTTPTransport) SetErrorHandler(handler transport.ErrorHandler) {
	t.errorHandler = handler
}

func (t *HTTPTransport) SetCloseHandler(handler transport.CloseHandler) {
	t.closeHandler = handler
}
