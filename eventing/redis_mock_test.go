package eventing

import (
	"context"
	"testing"

	"github.com/agentuity/go-common/logger"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func TestRedisSubscriberIsValid(t *testing.T) {
	t.Run("nil subscriber", func(t *testing.T) {
		var sub *redisSubscriber
		assert.False(t, sub.IsValid())
	})

	t.Run("nil pubsub", func(t *testing.T) {
		sub := &redisSubscriber{}
		assert.False(t, sub.IsValid())
	})
}

func TestRedisQueueSubscriberIsValid(t *testing.T) {
	t.Run("nil subscriber", func(t *testing.T) {
		var sub *redisQueueSubscriber
		assert.False(t, sub.IsValid())
	})

	t.Run("nil rdb", func(t *testing.T) {
		sub := &redisQueueSubscriber{}
		assert.False(t, sub.IsValid())
	})

	t.Run("not running", func(t *testing.T) {
		sub := &redisQueueSubscriber{}
		sub.running.Store(false)
		assert.False(t, sub.IsValid())
	})

	t.Run("valid", func(t *testing.T) {
		sub := &redisQueueSubscriber{}
		sub.running.Store(true)
		assert.True(t, sub.IsValid())
	})
}

func TestRedisMsgPayload(t *testing.T) {
	t.Run("Data", func(t *testing.T) {
		msg := &redisMsgPayload{InternalData: []byte("test data")}
		assert.Equal(t, []byte("test data"), msg.Data())
	})

	t.Run("Headers", func(t *testing.T) {
		headers := Headers{"key": "value"}
		msg := &redisMsgPayload{InternalHeaders: headers}
		assert.Equal(t, headers, msg.Headers())
	})

	t.Run("Subject", func(t *testing.T) {
		msg := &redisMsgPayload{subject: "test.subject"}
		assert.Equal(t, "test.subject", msg.Subject())
	})

	t.Run("Reply", func(t *testing.T) {
		called := false
		replier := func(ctx context.Context, data []byte, opts ...PublishOption) error {
			called = true
			return nil
		}
		msg := &redisMsgPayload{replier: replier}
		err := msg.Reply(context.Background(), []byte("reply"))
		assert.NoError(t, err)
		assert.True(t, called)
	})
}

func TestCheckForWildcards(t *testing.T) {
	t.Run("no wildcards", func(t *testing.T) {
		err := checkForWildcards("test.subject")
		assert.NoError(t, err)
	})

	t.Run("suffix wildcard", func(t *testing.T) {
		err := checkForWildcards("test.*")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "wildcards")
	})

	t.Run("middle wildcard", func(t *testing.T) {
		err := checkForWildcards("test.*.subject")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "wildcards")
	})
}

func TestNotReplyable(t *testing.T) {
	err := notReplyable(context.Background(), []byte("test"))
	assert.Equal(t, ErrNotReplyable, err)
}

func TestNewPubRedisMessage(t *testing.T) {
	t.Run("basic message", func(t *testing.T) {
		data := []byte("test data")
		msg := newPubRedisMessage(context.Background(), data, nil)

		assert.Equal(t, data, msg.InternalData)
		assert.NotNil(t, msg.InternalHeaders)
		assert.Empty(t, msg.InternalHeaders)
	})

	t.Run("with headers", func(t *testing.T) {
		data := []byte("test data")
		opts := []PublishOption{
			WithHeader("key1", "value1"),
			WithHeader("key2", "value2"),
		}

		msg := newPubRedisMessage(context.Background(), data, opts)

		assert.Equal(t, data, msg.InternalData)
		assert.Equal(t, "value1", msg.InternalHeaders["key1"])
		assert.Equal(t, "value2", msg.InternalHeaders["key2"])
	})

	t.Run("with invalid header", func(t *testing.T) {
		invalidOpt := func(o *publishOptions) {
			o.Headers = append(o.Headers, []string{"single"}) // Only one element
		}

		data := []byte("test data")
		opts := []PublishOption{invalidOpt}

		msg := newPubRedisMessage(context.Background(), data, opts)

		assert.Equal(t, data, msg.InternalData)
		assert.Empty(t, msg.InternalHeaders)
	})
}

func TestNewRedisClient(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	rdb := &redis.Client{}

	client, err := NewRedisClient(ctx, log, rdb)
	assert.NoError(t, err)
	assert.NotNil(t, client)

	err = client.Close()
	assert.NoError(t, err)
}
