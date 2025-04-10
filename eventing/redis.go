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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type redisMsgPayload struct {
	InternalData    []byte  `msgpack:"data"`
	InternalHeaders Headers `msgpack:"headers"`
	replier         func(ctx context.Context, data []byte, opts ...PublishOption) error
	subject         string

	options publishOptions
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

func newPubRedisMessage(ctx context.Context, data []byte, opts []PublishOption) redisMsgPayload {
	msg := redisMsgPayload{
		InternalData:    data,
		InternalHeaders: make(map[string]string),
		replier:         notReplyable,
	}

	// Apply publish options
	for _, opt := range opts {
		opt(&msg.options)
	}
	for _, header := range msg.options.Headers {
		if len(header) == 2 {
			msg.InternalHeaders[header[0]] = header[1]
		}
	}

	propagator.Inject(ctx, msg.InternalHeaders)

	return msg
}

func (c *redisEventingClient) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	spanCtx, span := tracer.Start(ctx, "Publish", trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(attribute.String("subject", subject)))
	defer span.End()
	if err := c.publish(spanCtx, subject, data, opts); err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return err
	}
	span.SetStatus(codes.Ok, "message published")
	return nil
}

func (c *redisEventingClient) publish(ctx context.Context, subject string, data []byte, opts []PublishOption) error {
	msg := newPubRedisMessage(ctx, data, append(opts, WithHeader(SubjectHeader, subject)))

	payload, err := msgpack.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	if err := c.rdb.Publish(ctx, subject, payload).Err(); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}

func (c *redisEventingClient) QueuePublish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	spanCtx, span := tracer.Start(ctx, "QueuePublish", trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(attribute.String("subject", subject)))
	defer span.End()

	if err := c.queuePublish(spanCtx, subject, data, opts); err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return err
	}
	span.SetStatus(codes.Ok, "message published")
	return nil
}

func (c *redisEventingClient) queuePublish(ctx context.Context, subject string, data []byte, opts []PublishOption) error {
	if err := checkForWildcards(subject); err != nil {
		return err
	}
	msg := newPubRedisMessage(ctx, data, opts)

	payload, err := msgpack.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	trim := int64(50)
	if msg.options.Trim != 0 {
		trim = msg.options.Trim
	}

	// Use XADD with MAXLEN to keep the stream size bounded
	return c.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: subject,
		Approx: true,
		MaxLen: trim,
		Values: map[string]interface{}{
			"payload": payload,
		},
	}).Err()
}

func (c *redisEventingClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration, opts ...PublishOption) (Message, error) {
	spanCtx, span := tracer.Start(ctx, "Request", trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(attribute.String("subject", subject)))
	defer span.End()

	msg, err := c.request(spanCtx, subject, data, timeout, false, opts...)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, err
	}
	span.SetStatus(codes.Ok, "message published")
	return msg, nil
}

func (c *redisEventingClient) QueueRequest(ctx context.Context, subject string, data []byte, timeout time.Duration, opts ...PublishOption) (Message, error) {
	spanCtx, span := tracer.Start(ctx, "QueueRequest", trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(attribute.String("subject", subject)))
	defer span.End()

	msg, err := c.request(spanCtx, subject, data, timeout, true, opts...)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, err
	}
	span.SetStatus(codes.Ok, "message published")
	return msg, nil
}

func newReplySubject() (string, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("failed to generate subject: %w", err)
	}
	return fmt.Sprintf("_INBOX.%s", u.String()), nil
}

var ErrRequestTimeout = errors.New("request timed out")

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

	requestContext, cancel := context.WithDeadlineCause(ctx, time.Now().Add(timeout), ErrRequestTimeout)
	defer cancel()

	// Subscribe to the reply subject
	sub := c.rdb.Subscribe(requestContext, replySubject)
	defer sub.Close()

	// Set up a goroutine to handle the reply
	go func() {
		ch := sub.Channel()
		select {
		case <-requestContext.Done():
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
		if err := c.queuePublish(requestContext, subject, data, opts); err != nil {
			return nil, fmt.Errorf("failed to queue publish: %w", err)
		}
	} else {
		if err := c.publish(requestContext, subject, data, opts); err != nil {
			return nil, fmt.Errorf("failed to publish: %w", err)
		}
	}

	// Wait for the reply with timeout
	select {
	case reply := <-replyChan:
		return reply, nil
	case <-requestContext.Done():
		return nil, requestContext.Err()
	}
}

func (c *redisEventingClient) newReplier(subject string) func(ctx context.Context, data []byte, opts ...PublishOption) error {
	return func(ctx context.Context, data []byte, opts ...PublishOption) error {
		spanCtx, span := tracer.Start(
			ctx,
			"Reply",
			trace.WithSpanKind(trace.SpanKindProducer),
			trace.WithAttributes(attribute.String("subject", subject)),
		)
		defer span.End()
		if err := c.publish(spanCtx, subject, data, opts); err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
			return err
		}
		span.SetStatus(codes.Ok, "message published")
		return nil
	}
}

func (c *redisEventingClient) decodeMessage(queueSubject string, payload []byte) (redisMsgPayload, error) {
	var msg redisMsgPayload
	if err := msgpack.Unmarshal(payload, &msg); err != nil {
		return redisMsgPayload{}, fmt.Errorf("failed to decode message: %w", err)
	}
	var subject string
	if queueSubject != "" {
		subject = queueSubject
	} else {
		if subj := msg.InternalHeaders[SubjectHeader]; subj != "" {
			subject = subj
		}
	}
	if subject == "" {
		return redisMsgPayload{}, fmt.Errorf("unable to determine subject from message")
	}
	msg.subject = subject
	return msg, nil
}

func (c *redisEventingClient) receiveMessage(ctx context.Context, queueSubject string, payload []byte, cb MessageCallback) {
	logger := c.logger.WithContext(ctx)

	msg, err := c.decodeMessage(queueSubject, payload)
	if err != nil {
		logger.Error(err.Error())
		return
	}
	// extract the trace context from the headers
	spanCtx, span := tracer.Start(
		propagator.Extract(ctx, msg.InternalHeaders),
		"receiveMessage",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("subject", msg.subject),
		),
	)
	defer span.End()

	if msg.InternalHeaders[ReplyHeader] != "" {
		msg.replier = c.newReplier(msg.InternalHeaders[ReplyHeader])
	} else {
		msg.replier = notReplyable
	}

	cb(spanCtx, &msg)
	span.SetStatus(codes.Ok, "message received")
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
			c.logger.Trace("unsubscribed from %s", subject)
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
				c.receiveMessage(ctx, "", []byte(redisMsg.Payload), cb)
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

func (c *redisEventingClient) initGroup(ctx context.Context, subject, queue string) (string, error) {
	if subject == "" {
		return "", fmt.Errorf("subject is required")
	}
	if queue == "" {
		return "", fmt.Errorf("queue is required")
	}

	if err := checkForWildcards(subject); err != nil {
		return "", err
	}

	// Create a consumer group if it doesn't exist
	if err := c.rdb.XGroupCreateMkStream(ctx, subject, queue, "$").Err(); err != nil && err != redis.Nil {
		if err.Error() == "BUSYGROUP Consumer Group name already exists" {
			// great!
		} else {
			return "", fmt.Errorf("failed to create consumer group: %w", err)
		}
	}

	// Generate a unique consumer ID
	consumer := fmt.Sprintf("%s-%d", queue, time.Now().UnixNano())
	return consumer, nil
}

func (c *redisEventingClient) QueueSubscribe(ctx context.Context, subject, queue string, cb MessageCallback) (Subscriber, error) {
	// Create a consumer group if it doesn't exist
	consumer, err := c.initGroup(ctx, subject, queue)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize consumer group: %w", err)
	}

	logger := c.logger.WithContext(ctx).With(map[string]any{"consumer": consumer, "subject": subject, "queue": queue})

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
			logger.Trace("unsubscribed")
			sub.running.Store(false)
			sub.wg.Done() // TODO: probably need to do this in the loop instead
		}()
		var failures int
		for {
			select {
			case <-ctx.Done():
				return
			default:
				logger.Trace("reading messages")
				// Read messages from the stream
				streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
					Group:    queue,
					Consumer: consumer,
					Streams:  []string{subject, ">"},
					Count:    10,              // Process up to 10 messages at a time
					Block:    time.Minute * 1, // Block for 1 minutes then loop
				}).Result()

				if err != nil {
					if err == redis.Nil {
						continue
					}
					if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
						continue
					}
					logger.Error("failed to read messages from %s: %s", subject, err)
					failures++
					time.Sleep(time.Second * 10)
					if failures > 100 {
						logger.Error("closing subscriber for %s: %s", subject, err)
						return
					}
					continue
				}

				for _, stream := range streams {
					for _, message := range stream.Messages {
						// Get the payload from the message
						payload, ok := message.Values["payload"].(string)
						if !ok {
							logger.Error("invalid message payload for %s: %v", subject, message.Values)
							continue
						}

						// Process the message
						c.receiveMessage(ctx, subject, []byte(payload), cb)

						// Acknowledge the message
						if err := c.rdb.XAck(ctx, subject, queue, message.ID).Err(); err != nil {
							logger.Error("failed to acknowledge message %s: %s", message.ID, err)
						}
					}
				}
			}
		}
	}()

	return sub, nil
}

type redisMessageSet struct {
	rdb      *redis.Client
	consumer string
	subject  string
	queue    string
	msgs     []Message
	ids      []string
}

func (m *redisMessageSet) Messages() []Message {
	return m.msgs
}

func (m *redisMessageSet) Ack(ctx context.Context) error {
	if len(m.ids) == 0 {
		// this is technically impossible because xreadgroup doesnt return an empty slice, it blocks until there are messages
		return nil
	}
	return m.rdb.XAck(ctx, m.subject, m.queue, m.ids...).Err()
}

func (c *redisEventingClient) QueueFetchMessages(_ctx context.Context, subject, queue string, count int64) (MessageSet, error) {
	spanCtx, span := tracer.Start(_ctx, "QueueFetchMessages", trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(attribute.String("subject", subject)))
	defer span.End()

	msgSet, err := c.queueFetchMessages(spanCtx, subject, queue, count)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetStatus(codes.Ok, "")
	return msgSet, nil
}

func (c *redisEventingClient) queueFetchMessages(ctx context.Context, subject, queue string, count int64) (MessageSet, error) {
	consumer, err := c.initGroup(ctx, subject, queue)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize consumer group: %w", err)
	}
	streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    queue,
		Consumer: consumer,
		Streams:  []string{subject, ">"},
		Block:    time.Millisecond * 500, // idk what a good block time is
		Count:    count,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return &redisMessageSet{}, nil // Return empty slice for no messages
		}
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	if len(streams) == 0 {
		return &redisMessageSet{}, nil
	}
	msgSet := redisMessageSet{
		rdb:      c.rdb,
		consumer: consumer,
		subject:  subject,
		queue:    queue,
	}
	stream := streams[0]
	for _, message := range stream.Messages {
		payload, ok := message.Values["payload"].(string)
		if !ok {
			c.logger.Error("invalid message payload for %s: %v", subject, message.Values)
			continue
		}

		// Process the message
		msg, err := c.decodeMessage(subject, []byte(payload))
		if err != nil {
			c.logger.Error(err.Error())
			continue
		}
		msgSet.msgs = append(msgSet.msgs, &msg)
		msgSet.ids = append(msgSet.ids, message.ID)

	}
	return &msgSet, nil
}

func (c *redisEventingClient) Close() error {
	c.cancel()
	return nil
}
