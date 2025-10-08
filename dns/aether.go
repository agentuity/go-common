package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	pb "github.com/agentuity/go-common/dns/proto"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type DNSBaseAction struct {
	MsgID  string `json:"msg_id"`
	Action string `json:"action"`
	Reply  string `json:"reply,omitempty"`
}

// DNSAddAction represents a DNS record addition request.
// Deprecated: This type will be replaced by the protobuf-generated types in dns/proto.
type DNSAddAction struct {
	DNSBaseAction
	Name  string `json:"name"`
	Type  string `json:"type,omitempty"`
	Value string `json:"value,omitempty"`
	// TTL is the DNS TTL for the record
	TTL time.Duration `json:"ttl,omitempty"`
	// Expires is the expiration time of the DNS record
	// if not provided the record will never expire
	Expires time.Duration `json:"expires,omitempty"`

	// Priority is the priority of the DNS record
	// only used for MX and SRV records
	Priority int `json:"priority,omitempty"`
	// Weight is the weight of the DNS record
	// only used for SRV records
	Weight int `json:"weight,omitempty"`
	// Port is the port of the DNS record
	// only used for SRV records
	Port int `json:"port,omitempty"`
}

func (a *DNSAddAction) WithTTL(ttl time.Duration) *DNSAddAction {
	a.TTL = ttl
	return a
}

// WithExpires sets the expiration
// if not provided the record will never expire
func (a *DNSAddAction) WithExpires(expires time.Duration) *DNSAddAction {
	a.Expires = expires
	return a
}

// WithPriority sets the priority of the DNS action
// only used for MX and SRV records
func (a *DNSAddAction) WithPriority(priority int) *DNSAddAction {
	a.Priority = priority
	return a
}

// WithWeight sets the weight of the DNS action
// only used for SRV records
func (a *DNSAddAction) WithWeight(weight int) *DNSAddAction {
	a.Weight = weight
	return a
}

// WithPort sets the port of the DNS action
// only used for SRV records
func (a *DNSAddAction) WithPort(port int) *DNSAddAction {
	a.Port = port
	return a
}

// DNSDeleteAction represents a DNS record deletion request.
// Deprecated: This type will be replaced by the protobuf-generated types in dns/proto.
type DNSDeleteAction struct {
	DNSBaseAction
	// Name is the name of the DNS record to delete.
	Name string `json:"name"`
	// IDs are the IDs of the DNS records to delete (within a name). This allows for clients to manage a specific record if they keep track of the ID.
	// If not provided, any name match will be deleted.
	IDs []string `json:"ids,omitempty"`
}

// DNSCertAction represents a certificate request for a domain.
// Deprecated: This type will be replaced by the protobuf-generated types in dns/proto.
type DNSCertAction struct {
	DNSBaseAction
	Name string `json:"name"`
}

type DNSResponse[T any] struct {
	MsgID   string `json:"msg_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    *T     `json:"data,omitempty"`
}

type DNSCert struct {
	Certificate []byte    `json:"certificate"`
	PrivateKey  []byte    `json:"private_key"`
	Expires     time.Time `json:"expires"`
	// Domain is the name for which the certificate is valid, it may be different from the name requested
	Domain string `json:"domain"`
}

type DNSRecord struct {
	IDs []string `json:"ids"`
}

func (r *DNSRecord) GetID() string {
	if len(r.IDs) == 0 {
		return ""
	}
	return r.IDs[0]
}

type DNSRecordType string

const (
	RecordTypeA     DNSRecordType = "A"
	RecordTypeAAAA  DNSRecordType = "AAAA"
	RecordTypeCNAME DNSRecordType = "CNAME"
	RecordTypeMX    DNSRecordType = "MX"
	RecordTypeNS    DNSRecordType = "NS"
	RecordTypeTXT   DNSRecordType = "TXT"
	RecordTypeSRV   DNSRecordType = "SRV"
)

// AddDNSAction adds a DNS action to the DNS server
func AddDNSAction(name string, recordType DNSRecordType, value string, ttl time.Duration, expires time.Duration) *DNSAddAction {
	return NewAddAction(name, recordType, value).WithTTL(ttl).WithExpires(expires)
}

// NewAddDNSAction creates a new DNS add action
func NewAddAction(name string, recordType DNSRecordType, value string) *DNSAddAction {
	action := &DNSAddAction{
		DNSBaseAction: DNSBaseAction{
			MsgID:  uuid.New().String(),
			Action: "add",
		},
		Name:  name,
		Type:  string(recordType),
		Value: value,
	}
	return action
}

// DeleteDNSAction deletes a DNS action from the DNS server
func DeleteDNSAction(name string, ids ...string) *DNSDeleteAction {
	action := &DNSDeleteAction{
		DNSBaseAction: DNSBaseAction{
			MsgID:  uuid.New().String(),
			Action: "delete",
		},
		Name: name,
		IDs:  ids,
	}
	return action
}

// CertRequestDNSAction requests a certificate from the DNS server
func CertRequestDNSAction(name string) *DNSCertAction {
	action := &DNSCertAction{
		DNSBaseAction: DNSBaseAction{
			MsgID:  uuid.New().String(),
			Action: "cert",
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
	// GetAction returns the action of the DNS action
	GetAction() string
}

// GetID returns the unique ID of the DNS action
func (a DNSBaseAction) GetID() string {
	return a.MsgID
}

// GetReply returns the reply of the DNS action
func (a DNSBaseAction) GetReply() string {
	return a.Reply
}

// SetReply sets the reply of the DNS action
func (a *DNSBaseAction) SetReply(reply string) {
	a.Reply = reply
}

// GetAction returns the action of the DNS action
func (a DNSBaseAction) GetAction() string {
	return a.Action
}

// transport is an interface for a transport layer for the DNS server
type transport interface {
	// Publish sends a DNS action and waits for a response
	Publish(ctx context.Context, action DNSAction) ([]byte, error)
	// PublishAsync sends a DNS action without waiting for a response
	PublishAsync(ctx context.Context, action DNSAction) error
}

type option struct {
	transport transport
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
//
// for Testing
func withTransport(t transport) optionHandler {
	return func(o *option) {
		o.transport = t
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

// WithGRPC uses a gRPC client as the transport for the DNS action
func WithGRPC(client pb.DNSServiceClient) optionHandler {
	return func(o *option) {
		o.transport = &grpcTransport{
			client: client,
		}
	}
}

// ActionFromChannel returns the action from the channel string
func ActionFromChannel(channel string) (string, error) {
	parts := strings.Split(channel, ":")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid channel: %s", channel)
	}
	return parts[2], nil
}

var ErrTimeout = errors.New("timeout")
var ErrTransportRequired = errors.New("transport is required")
var ErrClosed = errors.New("closed")

// TypedDNSAction is an interface for a DNS action that also specifies its expected response data type.
type TypedDNSAction[R any] interface {
	DNSAction
	// responseType is a marker method used for type inference.
	// It should return a zero value of the expected data type R for the DNSResponse.Data field.
	responseType() R
}

func (DNSAddAction) responseType() DNSRecord { return DNSRecord{} }
func (DNSDeleteAction) responseType() string { return "" }
func (DNSCertAction) responseType() DNSCert  { return DNSCert{} }

func NewDNSResponse[R any, T TypedDNSAction[R]](action T, data *R, err error) *DNSResponse[R] {
	var response DNSResponse[R]
	response.MsgID = action.GetID()
	if err != nil {
		response.Error = err.Error()
	} else {
		response.Success = true
	}
	response.Data = data
	return &response
}

// SendDNSAction sends a DNS action to the DNS server with a timeout. If the timeout is 0, the default timeout will be used.
func SendDNSAction[R any, T TypedDNSAction[R]](ctx context.Context, action T, opts ...optionHandler) (*R, error) {
	var o option
	o.timeout = DefaultDNSTimeout
	o.reply = true

	for _, opt := range opts {
		opt(&o)
	}

	if o.transport == nil {
		return nil, ErrTransportRequired
	}

	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	if !o.reply {
		return nil, o.transport.PublishAsync(ctx, action)
	}

	responseBytes, err := o.transport.Publish(ctx, action)
	if err != nil {
		return nil, err
	}

	var response DNSResponse[R]
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal dns action response: %w", err)
	}
	if !response.Success {
		return nil, errors.New(response.Error)
	}
	return response.Data, nil
}
