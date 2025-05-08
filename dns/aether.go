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

type DNSResponse struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
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

// SendDNSAction sends a DNS action to the DNS server with a timeout. If the timeout is 0, the default timeout will be used.
func SendDNSAction[T DNSAction](ctx context.Context, redis *redis.Client, action T, timeout time.Duration) error {
	if timeout == 0 {
		timeout = DefaultDNSTimeout
	}
	action.SetReply("aether:dns-reply:" + action.GetID())
	sub := redis.Subscribe(ctx, action.GetReply())
	defer sub.Close()
	if err := redis.Publish(ctx, "aether:dns-add:"+action.GetID(), cstr.JSONStringify(action)).Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case msg := <-sub.Channel():
		if msg == nil {
			return errors.New("closed")
		}
		var response DNSResponse
		if err := json.Unmarshal([]byte(msg.Payload), &response); err != nil {
			return fmt.Errorf("failed to unmarshal dns action response: %w", err)
		}
		if !response.Success {
			return errors.New(response.Error)
		}
	case <-time.After(timeout):
		return errors.New("timeout")
	}
	return nil
}
