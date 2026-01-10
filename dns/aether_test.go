package dns

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type testTransport struct {
	lastAction DNSAction
}

var _ transport = (*testTransport)(nil)

func (t *testTransport) Publish(ctx context.Context, action DNSAction) ([]byte, error) {
	t.lastAction = action

	// Return different responses based on action type
	switch action.(type) {
	case *DNSCertAction:
		var cert DNSCert
		cert.Certificate = []byte("cert")
		cert.Expires = time.Now().Add(time.Hour * 24 * 365 * 2)
		cert.PrivateKey = []byte("private")
		var response DNSResponse[DNSCert]
		response.Success = true
		response.Data = &cert
		return json.Marshal(response)
	case *DNSAddAction:
		var record DNSRecord
		record.IDs = []string{uuid.New().String()}
		var response DNSResponse[DNSRecord]
		response.Success = true
		response.Data = &record
		return json.Marshal(response)
	default:
		return nil, nil
	}
}

func (t *testTransport) PublishAsync(ctx context.Context, action DNSAction) error {
	t.lastAction = action
	return nil
}

func TestDNSAction(t *testing.T) {
	var transport testTransport

	action := &DNSAddAction{
		DNSBaseAction: DNSBaseAction{
			MsgID:  uuid.New().String(),
			Action: "add",
		},
		Name: "test",
	}

	reply, err := SendDNSAction(context.Background(), action, withTransport(&transport), WithTimeout(time.Second))
	if err != nil {
		t.Fatalf("failed to send dns action: %v", err)
	}

	assert.NoError(t, err)
	assert.NotNil(t, reply)
}

func TestDNSCertAction(t *testing.T) {
	var transport testTransport

	action := &DNSCertAction{
		DNSBaseAction: DNSBaseAction{
			MsgID:  uuid.New().String(),
			Action: "cert",
		},
		Name: "test",
	}

	reply, err := SendDNSAction(context.Background(), action, withTransport(&transport), WithTimeout(time.Second))
	if err != nil {
		t.Fatalf("failed to send dns cert action: %v", err)
	}

	assert.NoError(t, err)
	assert.NotNil(t, reply)
	assert.Equal(t, "cert", string(reply.Certificate))
	assert.Equal(t, "private", string(reply.PrivateKey))
	assert.True(t, reply.Expires.After(time.Now()))
	a, err := ActionFromChannel("aether:response:cert:123")
	assert.NoError(t, err)
	assert.Equal(t, "cert", a)
}
