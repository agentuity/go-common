package eventing

import (
	"context"
	"time"
)

// Message represents a message received from the event system
type Message interface {
	Data() []byte
	Headers() Headers
}

type msgReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Headers represents message headers that can be used for both map operations and propagation
type Headers map[string]string

func (h Headers) Get(key string) string {
	return h[key]
}

func (h Headers) Set(key string, value string) {
	h[key] = value
}

func (h Headers) Keys() []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	return keys
}

type MessageCallback func(ctx context.Context, msg Message)

type Subscriber interface {
	// Close stops the subscriber
	Close() error
}

// Client defines the interface for event clients
type Client interface {
	// Publish publishes a message to a subject
	Publish(ctx context.Context, subject string, data []byte) error
	// PublishQueue publishes a message to a subject in a consumer group named queue
	PublishQueue(ctx context.Context, subject string, data []byte) error
	// Request requests a message from a subject, and synchronously waits for a reply
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) (Message, error)
	// Subscribe subscribes to a subject
	Subscribe(ctx context.Context, subject string, cb MessageCallback) (Subscriber, error)
	// QueueSubscribe subscribes to a subject in a consumer group named queue
	QueueSubscribe(ctx context.Context, subject, queue string, cb MessageCallback) (Subscriber, error)
	// Close closes the client
	Close() error
}
