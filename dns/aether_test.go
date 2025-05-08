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
	publish   chan *Message
	subscribe chan *Message
}

var _ Transport = (*testTransport)(nil)

func (t *testTransport) Subscribe(ctx context.Context, channel string) Subscriber {
	t.subscribe = make(chan *Message, 1)
	t.publish = make(chan *Message, 1)
	return &testSubscriber{channel: channel, messages: t.subscribe}
}

func (t *testTransport) Publish(ctx context.Context, channel string, payload []byte) error {
	t.publish <- &Message{Payload: payload}
	var cert DNSCert
	cert.Certificate = []byte("cert")
	cert.Expires = time.Now().Add(time.Hour * 24 * 365 * 2)
	cert.PrivateKey = []byte("private")
	var response DNSResponse[DNSCert]
	response.Success = true
	response.Data = &cert
	response.ID = uuid.New().String()
	response.Error = ""
	response.Data = &cert
	responseBytes, err := json.Marshal(response)
	if err != nil {
		return err
	}
	t.subscribe <- &Message{Payload: responseBytes}
	return nil
}

type testSubscriber struct {
	channel  string
	messages chan *Message
}

var _ Subscriber = (*testSubscriber)(nil)

func (s *testSubscriber) Close() error {
	return nil
}

func (s *testSubscriber) Channel() <-chan *Message {
	return s.messages
}

func TestDNSAction(t *testing.T) {
	var transport testTransport

	action := &DNSAddAction{
		Name: "test",
	}

	reply, err := SendDNSAction[any](context.Background(), action, WithTransport(&transport), WithTimeout(time.Second))
	if err != nil {
		t.Fatalf("failed to send dns action: %v", err)
	}

	assert.NoError(t, err)
	assert.NotNil(t, reply)
}

func TestDNSCertAction(t *testing.T) {
	var transport testTransport

	action := &DNSCertAction{
		Name: "test",
	}

	reply, err := SendDNSAction[DNSCert](context.Background(), action, WithTransport(&transport), WithTimeout(time.Second))
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
