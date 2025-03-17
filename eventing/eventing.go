package eventing

import (
	"context"
	"time"
)

// Message represents a message received from the event system
type Message interface {
	Data() []byte
	Headers() Headers
	Reply(ctx context.Context, data []byte, opts ...PublishOption) error
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

type PublishOption func(*publishOptions)

type publishOptions struct {
	Headers [][]string
}

func WithHeader(key, value string) PublishOption {
	return func(o *publishOptions) {
		o.Headers = append(o.Headers, []string{key, value})
	}
}

// Client defines the interface for event clients
type Client interface {
	// Publish publishes a message to a subject
	Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error
	// PublishQueue publishes a message to a subject in a consumer group named queue
	PublishQueue(ctx context.Context, subject string, data []byte, opts ...PublishOption) error
	// Request requests a message from a subject, and synchronously waits for a reply
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration, opts ...PublishOption) (Message, error)
	// RequestQueue requests a message from a subject in a consumer group named queue, and synchronously waits for a reply
	RequestQueue(ctx context.Context, subject string, data []byte, timeout time.Duration, opts ...PublishOption) (Message, error)
	// Subscribe subscribes to a subject
	Subscribe(ctx context.Context, subject string, cb MessageCallback) (Subscriber, error)
	// QueueSubscribe subscribes to a subject in a consumer group named queue
	QueueSubscribe(ctx context.Context, subject, queue string, cb MessageCallback) (Subscriber, error)
	// Close closes the client
	Close() error
}
