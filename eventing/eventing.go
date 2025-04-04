package eventing

import (
	"context"
	"time"
)

var ReplyHeader = "reply-to"
var SubjectHeader = "subject"

// Message represents a message received from the event system
type Message interface {
	Data() []byte
	Headers() Headers
	Reply(ctx context.Context, data []byte, opts ...PublishOption) error
	Subject() string
}

type msgReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type MessageSet interface {
	Messages() []Message
	Ack(ctx context.Context) error
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

type MessageReplier func(ctx context.Context, data []byte, opts ...PublishOption) error

type Subscriber interface {
	// Close stops the subscriber
	Close() error
	// IsValid returns true if the subscriber is running, false if it has been closed, or is nil or is otherwise not in a running state
	IsValid() bool
	// CloseWithCallback closes the subscriber and calls the callback with the error if there is one
	CloseWithCallback(ctx context.Context, cb func(err error))
}

type PublishOption func(*publishOptions)

type publishOptions struct {
	Headers [][]string
	Trim    int64
}

func WithHeader(key, value string) PublishOption {
	return func(o *publishOptions) {
		o.Headers = append(o.Headers, []string{key, value})
	}
}

// sets the trim publish option, which is the number of old messages to trim from the stream after the message is published
func WithTrim(trim int64) PublishOption {
	return func(o *publishOptions) {
		o.Trim = trim
	}
}

// Client defines the interface for event clients
type Client interface {
	// Publish publishes a message to a subject
	Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error
	// QueuePublish publishes a message to a subject in a consumer group named queue
	QueuePublish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error
	// Request requests a message from a subject, and synchronously waits for a reply. All subscribers will receive the message and try to reply. You probably want to use QueueRequest instead.
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration, opts ...PublishOption) (Message, error)
	// QueueRequest requests a message from a subject in a consumer, and synchronously waits for a reply
	QueueRequest(ctx context.Context, subject string, data []byte, timeout time.Duration, opts ...PublishOption) (Message, error)
	// Subscribe subscribes to a subject
	Subscribe(ctx context.Context, subject string, cb MessageCallback) (Subscriber, error)
	// QueueSubscribe subscribes to a subject in a consumer group named queue
	QueueSubscribe(ctx context.Context, subject, queue string, cb MessageCallback) (Subscriber, error)
	// QueueFetchMessages fetches messages from a subject in a consumer group named queue, the messages must be acknowledged by calling the Ack method on the MessageSet
	QueueFetchMessages(ctx context.Context, subject, queue string, count int64) (MessageSet, error)
	// Close closes the client
	Close() error
}
