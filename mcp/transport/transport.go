package transport

import (
	"context"

	"github.com/agentuity/go-common/mcp/types"
)

type Transport interface {
	Start(ctx context.Context) error

	Send(ctx context.Context, message *types.JSONRPCMessage) error

	Close() error

	SetMessageHandler(handler MessageHandler)

	SetErrorHandler(handler ErrorHandler)

	SetCloseHandler(handler CloseHandler)
}

type MessageHandler func(message *types.JSONRPCMessage)

type ErrorHandler func(err error)

type CloseHandler func()
