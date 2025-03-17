package eventing

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vmihailenco/msgpack/v5"
)

type redisMessage struct {
	data    []byte
	headers Headers
}

func (m *redisMessage) Data() []byte {
	return m.data
}

func (m *redisMessage) Headers() Headers {
	return m.headers
}

type redisMsgPayload struct {
	Data    []byte            `msgpack:"data"`
	Headers map[string]string `msgpack:"headers"`
}

type redisSubscriber struct {
	pubsub *redis.PubSub
	ctx    context.Context
}

func (s *redisSubscriber) Close() error {
	return s.pubsub.Close()
}

type redisEventingClient struct {
	rdb    *redis.Client
	ctx    context.Context
	cancel context.CancelFunc
}

var _ Client = (*redisEventingClient)(nil)

func NewRedisClient(ctx context.Context, rdb *redis.Client) (Client, error) {
	ctx, cancel := context.WithCancel(ctx)
	client := &redisEventingClient{
		rdb:    rdb,
		ctx:    ctx,
		cancel: cancel,
	}

	return client, nil
}

func (c *redisEventingClient) Publish(ctx context.Context, subject string, data []byte) error {
	msg := redisMsgPayload{
		Data:    data,
		Headers: make(map[string]string),
	}

	payload, err := msgpack.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return c.rdb.Publish(ctx, subject, payload).Err()
}

func (c *redisEventingClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) (Message, error) {
	// Generate a unique reply subject
	replySubject := fmt.Sprintf("%s.reply.%d", subject, time.Now().UnixNano())

	// Create a channel to receive the reply
	replyChan := make(chan Message, 1)

	// Subscribe to the reply subject
	sub := c.rdb.Subscribe(ctx, replySubject)
	defer sub.Close()

	// Set up a goroutine to handle the reply
	go func() {
		ch := sub.Channel()
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}

			var redisMsg redisMsgPayload
			if err := msgpack.Unmarshal([]byte(msg.Payload), &redisMsg); err != nil {
				return
			}

			replyChan <- &redisMessage{
				data:    redisMsg.Data,
				headers: redisMsg.Headers,
			}
		}
	}()

	// Publish the request with the reply subject in headers
	msg := redisMsgPayload{
		Data: data,
		Headers: map[string]string{
			"reply-to": replySubject,
		},
	}

	payload, err := msgpack.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if err := c.rdb.Publish(ctx, subject, payload).Err(); err != nil {
		return nil, fmt.Errorf("failed to publish request: %w", err)
	}

	// Wait for the reply with timeout
	select {
	case reply := <-replyChan:
		return reply, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timed out after %v", timeout)
	}
}

func (c *redisEventingClient) Subscribe(subject string, cb MessageCallback) (Subscriber, error) {
	// Create a new PubSub instance for this subscription
	pubsub := c.rdb.Subscribe(c.ctx, subject)

	// Start a goroutine to handle messages for this subscription
	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case <-c.ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}

				var redisMsg redisMsgPayload
				if err := msgpack.Unmarshal([]byte(msg.Payload), &redisMsg); err != nil {
					continue
				}

				cb(c.ctx, &redisMessage{
					data:    redisMsg.Data,
					headers: redisMsg.Headers,
				})
			}
		}
	}()

	return &redisSubscriber{
		pubsub: pubsub,
		ctx:    c.ctx,
	}, nil
}

func (c *redisEventingClient) QueueSubscribe(subject, queue string, cb MessageCallback) (Subscriber, error) {
	// Create a new PubSub instance for this queue subscription
	pubsub := c.rdb.Subscribe(c.ctx, subject)

	// Start a goroutine to handle messages for this subscription
	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case <-c.ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}

				var redisMsg redisMsgPayload
				if err := msgpack.Unmarshal([]byte(msg.Payload), &redisMsg); err != nil {
					continue
				}

				cb(c.ctx, &redisMessage{
					data:    redisMsg.Data,
					headers: redisMsg.Headers,
				})
			}
		}
	}()

	return &redisSubscriber{
		pubsub: pubsub,
		ctx:    c.ctx,
	}, nil
}

func (c *redisEventingClient) Close() error {
	c.cancel()
	return nil
}
