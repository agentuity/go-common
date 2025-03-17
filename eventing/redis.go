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

type redisQueueSubscriber struct {
	streamKey string
	group     string
	consumer  string
	rdb       *redis.Client
	ctx       context.Context
}

func (s *redisQueueSubscriber) Close() error {
	// Remove the consumer from the group
	return s.rdb.XGroupDelConsumer(s.ctx, s.streamKey, s.group, s.consumer).Err()
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

func (c *redisEventingClient) PublishQueue(ctx context.Context, subject string, data []byte) error {
	msg := redisMsgPayload{
		Data:    data,
		Headers: make(map[string]string),
	}

	payload, err := msgpack.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Use XADD with MAXLEN to keep the stream size bounded
	return c.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: subject,
		Approx: true,
		MaxLen: 50,
		Values: map[string]interface{}{
			"payload": payload,
		},
	}).Err()
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

func (c *redisEventingClient) Subscribe(ctx context.Context, subject string, cb MessageCallback) (Subscriber, error) {
	// Create a new PubSub instance for this subscription
	pubsub := c.rdb.Subscribe(ctx, subject)

	// Start a goroutine to handle messages for this subscription
	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}

				var redisMsg redisMsgPayload
				if err := msgpack.Unmarshal([]byte(msg.Payload), &redisMsg); err != nil {
					continue
				}

				cb(ctx, &redisMessage{
					data:    redisMsg.Data,
					headers: redisMsg.Headers,
				})
			}
		}
	}()

	return &redisSubscriber{
		pubsub: pubsub,
		ctx:    ctx,
	}, nil
}

func (c *redisEventingClient) QueueSubscribe(ctx context.Context, subject, queue string, cb MessageCallback) (Subscriber, error) {
	// Create a consumer group if it doesn't exist
	if err := c.rdb.XGroupCreateMkStream(ctx, subject, queue, "$").Err(); err != nil && err != redis.Nil {
		if err.Error() == "BUSYGROUP Consumer Group name already exists" {
			// great!
		} else {
			return nil, fmt.Errorf("failed to create consumer group: %w", err)
		}
	}

	// Generate a unique consumer ID
	consumer := fmt.Sprintf("%s-%d", queue, time.Now().UnixNano())

	// Create the subscriber
	sub := &redisQueueSubscriber{
		streamKey: subject,
		group:     queue,
		consumer:  consumer,
		rdb:       c.rdb,
		ctx:       ctx,
	}

	// Start a goroutine to handle messages for this subscription
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Read messages from the stream
				streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
					Group:    queue,
					Consumer: consumer,
					Streams:  []string{subject, ">"},
					Count:    10, // Process up to 10 messages at a time
					Block:    0,  // Block indefinitely
				}).Result()

				if err != nil {
					if err == redis.Nil {
						continue
					}
					return
				}

				for _, stream := range streams {
					for _, message := range stream.Messages {
						// Get the payload from the message
						payload, ok := message.Values["payload"].(string)
						if !ok {
							continue
						}

						var redisMsg redisMsgPayload
						if err := msgpack.Unmarshal([]byte(payload), &redisMsg); err != nil {
							continue
						}

						// Process the message
						cb(ctx, &redisMessage{
							data:    redisMsg.Data,
							headers: redisMsg.Headers,
						})

						// Acknowledge the message
						c.rdb.XAck(ctx, subject, queue, message.ID)
					}
				}
			}
		}
	}()

	return sub, nil
}

func (c *redisEventingClient) Close() error {
	c.cancel()
	return nil
}
