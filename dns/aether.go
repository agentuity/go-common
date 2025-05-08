package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	cstr "github.com/agentuity/go-common/string"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type DNSBaseAction struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	Reply  string `json:"reply,omitempty"`
}

type DNSAddAction struct {
	DNSBaseAction
	Name    string        `json:"name"`
	Type    string        `json:"type,omitempty"`
	Value   string        `json:"value,omitempty"`
	TTL     time.Duration `json:"ttl,omitempty"`
	Expires time.Duration `json:"expires,omitempty"`
	TLSCert string        `json:"tls_cert,omitempty"`
}

type DNSDeleteAction struct {
	DNSBaseAction
	Name string `json:"name"`
}

type DNSCertAction struct {
	DNSBaseAction
	Name string `json:"name"`
}

type DNSResponse[T any] struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    *T     `json:"data,omitempty"`
}

type DNSCert struct {
	Certificate []byte    `json:"certificate"`
	PrivateKey  []byte    `json:"private_key"`
	Expires     time.Time `json:"expires"`
}

type DNSCertResponse struct {
	DNSResponse[DNSCert]
}

type DNSRecordType string

const (
	RecordTypeA     DNSRecordType = "A"
	RecordTypeAAAA  DNSRecordType = "AAAA"
	RecordTypeCNAME DNSRecordType = "CNAME"
	RecordTypeMX    DNSRecordType = "MX"
	RecordTypeNS    DNSRecordType = "NS"
	RecordTypeTXT   DNSRecordType = "TXT"
)

// AddDNSAction adds a DNS action to the DNS server
func AddDNSAction(name string, recordType DNSRecordType, value string, ttl time.Duration, expires time.Duration, tlsCert string) *DNSAddAction {
	action := &DNSAddAction{
		DNSBaseAction: DNSBaseAction{
			ID:     uuid.New().String(),
			Action: "add",
		},
		Name:    name,
		Type:    string(recordType),
		Value:   value,
		TTL:     ttl,
		Expires: expires,
		TLSCert: tlsCert,
	}
	return action
}

// DeleteDNSAction deletes a DNS action from the DNS server
func DeleteDNSAction(name string) *DNSDeleteAction {
	action := &DNSDeleteAction{
		DNSBaseAction: DNSBaseAction{
			ID:     uuid.New().String(),
			Action: "delete",
		},
		Name: name,
	}
	return action
}

// CertRequestDNSAction requests a certificate from the DNS server
func CertRequestDNSAction(name string) *DNSCertAction {
	action := &DNSCertAction{
		DNSBaseAction: DNSBaseAction{
			ID:     uuid.New().String(),
			Action: "cert-request",
		},
		Name: name,
	}
	return action
}

// DefaultDNSTimeout is the default timeout for a DNS action which is 10 seconds
const DefaultDNSTimeout = 10 * time.Second

// DNSAction is an interface for a DNS action
type DNSAction interface {
	// GetID returns the unique ID of the DNS action
	GetID() string
	// GetReply returns the reply of the DNS action
	GetReply() string
	// SetReply sets the reply of the DNS action
	SetReply(string)
}

// GetID returns the unique ID of the DNS action
func (a DNSBaseAction) GetID() string {
	return a.ID
}

// GetReply returns the reply of the DNS action
func (a DNSBaseAction) GetReply() string {
	return a.Reply
}

// SetReply sets the reply of the DNS action
func (a *DNSBaseAction) SetReply(reply string) {
	a.Reply = reply
}

// Transport is an interface for a transport layer for the DNS server
type Transport interface {
	Subscribe(ctx context.Context, channel string) Subscriber
	Publish(ctx context.Context, channel string, payload []byte) error
}

// Message is a message from the transport layer
type Message struct {
	Payload []byte
}

// Subscriber is an interface for a subscriber to the transport layer
type Subscriber interface {
	// Close closes the subscriber
	Close() error
	// Channel returns a channel of messages
	Channel() <-chan *Message
}

type option struct {
	transport Transport
	timeout   time.Duration
	reply     bool
}

type optionHandler func(*option)

// WithReply sets whether the DNS action should wait for a reply from the DNS server
func WithReply(reply bool) optionHandler {
	return func(o *option) {
		o.reply = reply
	}
}

// WithTransport sets a custom transport for the DNS action
func WithTransport(transport Transport) optionHandler {
	return func(o *option) {
		o.transport = transport
	}
}

// WithTimeout sets a custom timeout for the DNS action
func WithTimeout(timeout time.Duration) optionHandler {
	return func(o *option) {
		o.timeout = timeout
	}
}

// WithRedis uses a redis client as the transport for the DNS action
func WithRedis(redis *redis.Client) optionHandler {
	return func(o *option) {
		o.transport = &redisTransport{redis: redis}
	}
}

type redisSubscriber struct {
	sub *redis.PubSub
}

var _ Subscriber = (*redisSubscriber)(nil)

func (s *redisSubscriber) Close() error {
	return s.sub.Close()
}

func (s *redisSubscriber) Channel() <-chan *Message {
	ch := make(chan *Message)
	go func() {
		for msg := range s.sub.Channel() {
			ch <- &Message{Payload: []byte(msg.Payload)}
		}
	}()
	return ch
}

type redisTransport struct {
	redis *redis.Client
}

var _ Transport = (*redisTransport)(nil)

func (t *redisTransport) Subscribe(ctx context.Context, channel string) Subscriber {
	return &redisSubscriber{sub: t.redis.Subscribe(ctx, channel)}
}

func (t *redisTransport) Publish(ctx context.Context, channel string, payload []byte) error {
	return t.redis.Publish(ctx, channel, payload).Err()
}

var ErrTimeout = errors.New("timeout")
var ErrTransportRequired = errors.New("transport is required")
var ErrClosed = errors.New("closed")

// SendDNSAction sends a DNS action to the DNS server with a timeout. If the timeout is 0, the default timeout will be used.
func SendDNSAction[R any, T DNSAction](ctx context.Context, action T, opts ...optionHandler) (*R, error) {
	var o option
	o.timeout = DefaultDNSTimeout
	o.reply = true

	for _, opt := range opts {
		opt(&o)
	}

	if o.transport == nil {
		return nil, ErrTransportRequired
	}

	var sub Subscriber

	if o.reply {
		action.SetReply("aether:dns-reply:" + action.GetID())
		sub = o.transport.Subscribe(ctx, action.GetReply())
		defer sub.Close()
	}

	if err := o.transport.Publish(ctx, "aether:dns-add:"+action.GetID(), []byte(cstr.JSONStringify(action))); err != nil {
		return nil, err
	}

	if o.reply {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case msg := <-sub.Channel():
			if msg == nil {
				return nil, ErrClosed
			}
			var response DNSResponse[R]
			if err := json.Unmarshal([]byte(msg.Payload), &response); err != nil {
				return nil, fmt.Errorf("failed to unmarshal dns action response: %w", err)
			}
			if !response.Success {
				return nil, errors.New(response.Error)
			}
			return response.Data, nil
		case <-time.After(o.timeout):
			return nil, ErrTimeout
		}
	}

	return nil, nil
}
