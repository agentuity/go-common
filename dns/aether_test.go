package dns

import (
	"context"
	"testing"
	"time"

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
	t.subscribe <- &Message{Payload: []byte(`{"success": true}`)}
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
	assert.Nil(t, reply)
}
