package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentuity/go-common/logger"
)

func TestNewHTTPTransport(t *testing.T) {
	log := logger.NewConsoleLogger()
	transport := NewHTTPTransport("localhost:8080", "/mcp", log)
	
	if transport == nil {
		t.Fatal("Expected non-nil transport")
	}
	
	if transport.path != "/mcp" {
		t.Errorf("Expected path to be /mcp, got %s", transport.path)
	}
	
	if transport.sessionHeader != DefaultSessionHeaderName {
		t.Errorf("Expected session header to be %s, got %s", DefaultSessionHeaderName, transport.sessionHeader)
	}
	
	if transport.sseRetryMs != DefaultSSERetryInterval {
		t.Errorf("Expected SSE retry interval to be %d, got %d", DefaultSSERetryInterval, transport.sseRetryMs)
	}
	
	if transport.userAgent != DefaultUserAgent {
		t.Errorf("Expected user agent to be %s, got %s", DefaultUserAgent, transport.userAgent)
	}
}

func TestWithUserAgent(t *testing.T) {
	log := logger.NewConsoleLogger()
	customUserAgent := "Custom-User-Agent/1.0"
	transport := NewHTTPTransport("localhost:8080", "/mcp", log, WithUserAgent(customUserAgent))
	
	if transport.userAgent != customUserAgent {
		t.Errorf("Expected user agent to be %s, got %s", customUserAgent, transport.userAgent)
	}
}

func TestWithServeMux(t *testing.T) {
	log := logger.NewConsoleLogger()
	mux := http.NewServeMux()
	transport := NewHTTPTransport("localhost:8080", "/mcp", log, WithServeMux(mux))
	
	if transport.mux != mux {
		t.Error("Expected transport to use the provided ServeMux")
	}
}

func TestInMemorySessionStore(t *testing.T) {
	store := NewInMemorySessionStore()
	
	session := store.GetOrCreateSession("test-session")
	if session == nil {
		t.Fatal("Expected non-nil session")
	}
	
	if session.ID != "test-session" {
		t.Errorf("Expected session ID to be test-session, got %s", session.ID)
	}
	
	sameSession := store.GetOrCreateSession("test-session")
	if sameSession != session {
		t.Error("Expected to get the same session instance")
	}
	
	store.CloseSession("test-session")
	sessions := store.Sessions()
	if _, exists := sessions["test-session"]; exists {
		t.Error("Expected session to be removed after closing")
	}
}

func TestHTTPTransportStart(t *testing.T) {
	log := logger.NewConsoleLogger()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	
	transport := NewHTTPTransport(server.URL[7:], "/mcp", log)
	
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	if err := transport.Start(ctx); err != nil {
		t.Fatalf("Failed to start transport: %v", err)
	}
	
	<-ctx.Done()
}
