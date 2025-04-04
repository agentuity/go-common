package eventing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHeaders(t *testing.T) {
	t.Run("Get", func(t *testing.T) {
		h := Headers{"key": "value"}
		assert.Equal(t, "value", h.Get("key"))
		assert.Equal(t, "", h.Get("nonexistent"))
	})

	t.Run("Set", func(t *testing.T) {
		h := Headers{}
		h.Set("key", "value")
		assert.Equal(t, "value", h.Get("key"))

		h.Set("key", "new-value")
		assert.Equal(t, "new-value", h.Get("key"))
	})

	t.Run("Keys", func(t *testing.T) {
		h := Headers{"key1": "value1", "key2": "value2"}
		keys := h.Keys()
		assert.Len(t, keys, 2)
		assert.Contains(t, keys, "key1")
		assert.Contains(t, keys, "key2")
	})
}

func TestWithHeader(t *testing.T) {
	opts := &publishOptions{}

	WithHeader("key1", "value1")(opts)
	assert.Len(t, opts.Headers, 1)
	assert.Equal(t, []string{"key1", "value1"}, opts.Headers[0])

	WithHeader("key2", "value2")(opts)
	assert.Len(t, opts.Headers, 2)
	assert.Equal(t, []string{"key2", "value2"}, opts.Headers[1])
}

type MockMessage struct {
	data    []byte
	headers Headers
	subject string
}

func (m *MockMessage) Data() []byte {
	return m.data
}

func (m *MockMessage) Headers() Headers {
	return m.headers
}

func (m *MockMessage) Subject() string {
	return m.subject
}

func (m *MockMessage) Reply(ctx context.Context, data []byte, opts ...PublishOption) error {
	return nil
}

func TestMockMessage(t *testing.T) {
	msg := &MockMessage{
		data:    []byte("test data"),
		headers: Headers{"key": "value"},
		subject: "test.subject",
	}

	assert.Equal(t, []byte("test data"), msg.Data())
	assert.Equal(t, Headers{"key": "value"}, msg.Headers())
	assert.Equal(t, "test.subject", msg.Subject())

	err := msg.Reply(context.Background(), []byte("reply data"))
	assert.NoError(t, err)
}

type MockSubscriber struct {
	closed   bool
	valid    bool
	closeErr error
}

func (s *MockSubscriber) Close() error {
	s.closed = true
	s.valid = false
	return s.closeErr
}

func (s *MockSubscriber) IsValid() bool {
	return s.valid
}

func (s *MockSubscriber) CloseWithCallback(ctx context.Context, cb func(err error)) {
	cb(s.Close())
}

func TestMockSubscriber(t *testing.T) {
	t.Run("Close", func(t *testing.T) {
		sub := &MockSubscriber{valid: true}
		err := sub.Close()
		assert.NoError(t, err)
		assert.True(t, sub.closed)
		assert.False(t, sub.valid)
	})

	t.Run("CloseWithError", func(t *testing.T) {
		expectedErr := assert.AnError
		sub := &MockSubscriber{valid: true, closeErr: expectedErr}
		var receivedErr error
		sub.CloseWithCallback(context.Background(), func(err error) {
			receivedErr = err
		})
		assert.Equal(t, expectedErr, receivedErr)
		assert.True(t, sub.closed)
		assert.False(t, sub.valid)
	})

	t.Run("IsValid", func(t *testing.T) {
		sub := &MockSubscriber{valid: true}
		assert.True(t, sub.IsValid())

		sub.valid = false
		assert.False(t, sub.IsValid())
	})
}

type MockClient struct {
	publishErr      error
	queuePublishErr error
	requestMsg      Message
	requestErr      error
	queueRequestMsg Message
	queueRequestErr error
	subscribeResult Subscriber
	subscribeErr    error
	queueSubResult  Subscriber
	queueSubErr     error
	closeErr        error
}

func (c *MockClient) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	return c.publishErr
}

func (c *MockClient) QueuePublish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	return c.queuePublishErr
}

func (c *MockClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration, opts ...PublishOption) (Message, error) {
	return c.requestMsg, c.requestErr
}

func (c *MockClient) QueueRequest(ctx context.Context, subject string, data []byte, timeout time.Duration, opts ...PublishOption) (Message, error) {
	return c.queueRequestMsg, c.queueRequestErr
}

func (c *MockClient) Subscribe(ctx context.Context, subject string, cb MessageCallback) (Subscriber, error) {
	return c.subscribeResult, c.subscribeErr
}

func (c *MockClient) QueueSubscribe(ctx context.Context, subject, queue string, cb MessageCallback) (Subscriber, error) {
	return c.queueSubResult, c.queueSubErr
}

func (c *MockClient) Close() error {
	return c.closeErr
}

func TestMockClient(t *testing.T) {
	mockMsg := &MockMessage{data: []byte("test")}
	mockSub := &MockSubscriber{valid: true}

	client := &MockClient{
		requestMsg:      mockMsg,
		queueRequestMsg: mockMsg,
		subscribeResult: mockSub,
		queueSubResult:  mockSub,
	}

	err := client.Publish(context.Background(), "test", []byte("data"))
	assert.NoError(t, err)

	err = client.QueuePublish(context.Background(), "test", []byte("data"))
	assert.NoError(t, err)

	msg, err := client.Request(context.Background(), "test", []byte("data"), time.Second)
	assert.NoError(t, err)
	assert.Equal(t, mockMsg, msg)

	msg, err = client.QueueRequest(context.Background(), "test", []byte("data"), time.Second)
	assert.NoError(t, err)
	assert.Equal(t, mockMsg, msg)

	sub, err := client.Subscribe(context.Background(), "test", func(ctx context.Context, msg Message) {})
	assert.NoError(t, err)
	assert.Equal(t, mockSub, sub)

	sub, err = client.QueueSubscribe(context.Background(), "test", "queue", func(ctx context.Context, msg Message) {})
	assert.NoError(t, err)
	assert.Equal(t, mockSub, sub)

	err = client.Close()
	assert.NoError(t, err)
}
