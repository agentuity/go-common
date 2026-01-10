package dns

import (
	"context"
	"fmt"

	cstr "github.com/agentuity/go-common/string"
	"github.com/redis/go-redis/v9"
)

type redisTransport struct {
	redis *redis.Client
}

var _ transport = (*redisTransport)(nil)

func (t *redisTransport) Publish(ctx context.Context, action DNSAction) ([]byte, error) {
	id := action.GetID()
	if id == "" {
		return nil, fmt.Errorf("message ID not found")
	}

	replyChannel := "aether:response:" + action.GetAction() + ":" + id
	action.SetReply(replyChannel)

	sub := t.redis.Subscribe(ctx, replyChannel)
	defer sub.Close()

	if err := t.redis.Publish(ctx, "aether:request:"+action.GetAction()+":"+id, cstr.JSONStringify(action)).Err(); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-sub.Channel():
		if msg == nil {
			return nil, ErrClosed
		}
		return []byte(msg.Payload), nil
	}
}

func (t *redisTransport) PublishAsync(ctx context.Context, action DNSAction) error {
	id := action.GetID()
	if id == "" {
		return fmt.Errorf("message ID not found")
	}

	return t.redis.Publish(ctx, "aether:request:"+action.GetAction()+":"+id, cstr.JSONStringify(action)).Err()
}
