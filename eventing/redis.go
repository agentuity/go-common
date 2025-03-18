package eventing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentuity/go-common/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/vmihailenco/msgpack/v5"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type redisMsgPayload struct {
	InternalData    []byte  `msgpack:"data"`
	InternalHeaders Headers `msgpack:"headers"`
	replier         func(ctx context.Context, data []byte, opts ...PublishOption) error
	subject         string
}

func (m *redisMsgPayload) Data() []byte {
	return m.InternalData
}

func (m *redisMsgPayload) Headers() Headers {
	return m.InternalHeaders
}

func (m *redisMsgPayload) Subject() string {
	return m.subject
}

func (m *redisMsgPayload) Reply(ctx context.Context, data []byte, opts ...PublishOption) error {
	return m.replier(ctx, data, opts...)
}

type redisSubscriber struct {
	pubsub  *redis.PubSub
	ctx     context.Context
	wg      sync.WaitGroup
	running atomic.Bool
}

func (s *redisSubscriber) Close() error {
	if err := s.pubsub.Close(); err != nil {
		return err
	}
	s.wg.Wait()
	s.running.Store(false)
	return nil
}

func (s *redisSubscriber) CloseWithCallback(ctx context.Context, cb func(err error)) {
	cb(s.Close())
}

func (s *redisSubscriber) IsValid() bool {
	if s == nil {
		return false
	}
	if s.pubsub == nil {
		return false
	}
	return s.running.Load()
}

type redisQueueSubscriber struct {
	streamKey string
	group     string
	consumer  string
	rdb       *redis.Client
	ctx       context.Context
	running   atomic.Bool
	wg        sync.WaitGroup
}

func (s *redisQueueSubscriber) Close() error {
	// Remove the consumer from the group
	return s.rdb.XGroupDelConsumer(s.ctx, s.streamKey, s.group, s.consumer).Err()
}

func (s *redisQueueSubscriber) CloseWithCallback(ctx context.Context, cb func(err error)) {
	cb(s.Close())
}

func (s *redisQueueSubscriber) IsValid() bool {
	if s == nil {
		return false
	}
	if s.rdb == nil {
		return false
	}
	return s.running.Load()
}

type redisEventingClient struct {
	rdb    *redis.Client
	ctx    context.Context
	cancel context.CancelFunc
	logger logger.Logger
}

var _ Client = (*redisEventingClient)(nil)

func NewRedisClient(ctx context.Context, logger logger.Logger, rdb *redis.Client) (Client, error) {
	ctx, cancel := context.WithCancel(ctx)
	client := &redisEventingClient{
		rdb:    rdb,
		ctx:    ctx,
		cancel: cancel,
		logger: logger.With(map[string]interface{}{"component": "eventing"}),
	}

	return client, nil
}

var ErrNotReplyable = errors.New("message is not replyable")

func notReplyable(ctx context.Context, data []byte, opts ...PublishOption) error {
	return ErrNotReplyable
}

func newPubRedisMessage(data []byte, opts []PublishOption) redisMsgPayload {
	msg := redisMsgPayload{
		InternalData:    data,
		InternalHeaders: make(map[string]string),
		replier:         notReplyable,
	}

	// Apply publish options
	options := &publishOptions{}
	for _, opt := range opts {
		opt(options)
	}
	for _, header := range options.Headers {
		if len(header) == 2 {
			msg.InternalHeaders[header[0]] = header[1]
		}
	}

	return msg
}

func (c *redisEventingClient) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	msg := newPubRedisMessage(data, append(opts, WithHeader(SubjectHeader, subject)))
	logger := c.logger.WithContext(ctx)
	// inject the trace context into the headers before starting a span
	propagator.Inject(ctx, msg.InternalHeaders)

	spanCtx, span := tracer.Start(ctx, "Publish", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	payload, err := msgpack.Marshal(msg)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	logger.Trace("publishing message to %s", subject)
	if err := c.rdb.Publish(spanCtx, subject, payload).Err(); err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return fmt.Errorf("failed to publish message: %w", err)
	}

	span.SetStatus(codes.Ok, "message published")
	return nil
}

func (c *redisEventingClient) QueuePublish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	if err := checkForWildcards(subject); err != nil {
		return err
	}

	msg := newPubRedisMessage(data, opts)
	// inject the trace context into the headers before starting a span
	propagator.Inject(ctx, msg.InternalHeaders)

	spanCtx, span := tracer.Start(ctx, "QueuePublish", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	payload, err := msgpack.Marshal(msg)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Use XADD with MAXLEN to keep the stream size bounded
	return c.rdb.XAdd(spanCtx, &redis.XAddArgs{
		Stream: subject,
		Approx: true,
		MaxLen: 50,
		Values: map[string]interface{}{
			"payload": payload,
		},
	}).Err()
}

func (c *redisEventingClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration, opts ...PublishOption) (Message, error) {
	return c.request(ctx, subject, data, timeout, false, opts...)
}

func (c *redisEventingClient) QueueRequest(ctx context.Context, subject string, data []byte, timeout time.Duration, opts ...PublishOption) (Message, error) {
	return c.request(ctx, subject, data, timeout, true, opts...)
}

func newReplySubject() (string, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("failed to generate subject: %w", err)
	}
	return fmt.Sprintf("_INBOX.%s", u.String()), nil
}

// request is a helper function to request a message from a subject
// if queue is nil, the message is published using pubsub
// if queue is not nil, the message is published using streams
// replies are always sent using pubsub
func (c *redisEventingClient) request(ctx context.Context, subject string, data []byte, timeout time.Duration, queue bool, opts ...PublishOption) (Message, error) {
	if subject == "" {
		return nil, fmt.Errorf("subject is required")
	}

	// Generate a unique reply subject
	replySubject, err := newReplySubject()
	if err != nil {
		return nil, fmt.Errorf("failed to generate reply subject: %w", err)
	}

	// Create a channel to receive the reply
	replyChan := make(chan *redisMsgPayload, 1)

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

			replyChan <- &redisMsg
		}
	}()

	opts = append(opts, WithHeader(ReplyHeader, replySubject))

	if queue {
		if err := c.QueuePublish(ctx, subject, data, opts...); err != nil {
			return nil, fmt.Errorf("failed to queue publish: %w", err)
		}
	} else {
		if err := c.Publish(ctx, subject, data, opts...); err != nil {
			return nil, fmt.Errorf("failed to publish: %w", err)
		}
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

func (c *redisEventingClient) internalCallback(ctx context.Context, subject string, payload []byte, cb MessageCallback) {
	logger := c.logger.WithContext(ctx)
	var msg redisMsgPayload
	if err := msgpack.Unmarshal(payload, &msg); err != nil {
		logger.Error("failed to decode message %s", err)
		return
	}
	if subject != "" {
		subject = msg.subject
	} else if subj := msg.InternalHeaders[SubjectHeader]; subj != "" {
		subject = subj
	}
	if subject == "" {
		logger.Error("unable to determine subject from message")
		return
	}
	msg.subject = subject
	// extract the trace context from the headers
	spanCtx, span := tracer.Start(
		propagator.Extract(ctx, msg.InternalHeaders),
		"internalCallback",
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()

	if msg.InternalHeaders[ReplyHeader] != "" {
		msg.replier = func(ctx context.Context, data []byte, opts ...PublishOption) error {
			return c.Publish(ctx, msg.InternalHeaders[ReplyHeader], data, opts...)
		}
	} else {
		msg.replier = notReplyable
	}

	cb(spanCtx, &msg)
}

func (c *redisEventingClient) Subscribe(ctx context.Context, subject string, cb MessageCallback) (Subscriber, error) {
	// Create a new PubSub instance for this subscription
	pubsub := c.rdb.PSubscribe(ctx)
	if err := pubsub.PSubscribe(ctx, subject); err != nil {
		return nil, fmt.Errorf("failed to subscribe: %w", err)
	}

	c.logger.Trace("subscribed to %s", subject)
	sub := redisSubscriber{
		pubsub: pubsub,
		ctx:    ctx,
	}
	sub.running.Store(true)
	sub.wg.Add(1)
	go func() {
		ch := pubsub.Channel()
		defer func() {
			sub.wg.Done()
			sub.running.Store(false)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case redisMsg, ok := <-ch:
				if !ok {
					return
				}
				// let it get the subject from the message
				c.internalCallback(ctx, "", []byte(redisMsg.Payload), cb)
			}
		}
	}()

	return &sub, nil
}

func checkForWildcards(subject string) error {
	if strings.HasSuffix(subject, ".*") || strings.Contains(subject, ".*.") {
		return fmt.Errorf("redis streams do not support wildcards")
	}
	return nil
}

func (c *redisEventingClient) QueueSubscribe(ctx context.Context, subject, queue string, cb MessageCallback) (Subscriber, error) {
	if subject == "" {
		return nil, fmt.Errorf("subject is required")
	}
	if queue == "" {
		return nil, fmt.Errorf("queue is required")
	}

	if err := checkForWildcards(subject); err != nil {
		return nil, err
	}

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
	sub.running.Store(true)

	// Start a goroutine to handle messages for this subscription
	sub.wg.Add(1)
	go func() {
		defer func() {
			sub.running.Store(false)
			sub.wg.Done() // TODO: probably need to do this in the loop instead
		}()
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

						// Process the message
						c.internalCallback(ctx, subject, []byte(payload), cb)

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
