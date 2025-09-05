package main

import (
	"fmt"

	_ "github.com/agentuity/go-common/authentication"
	_ "github.com/agentuity/go-common/cache"
	_ "github.com/agentuity/go-common/compress"
	_ "github.com/agentuity/go-common/crypto"
	_ "github.com/agentuity/go-common/dns"
	_ "github.com/agentuity/go-common/env"
	_ "github.com/agentuity/go-common/eventing"
	_ "github.com/agentuity/go-common/llm"
	_ "github.com/agentuity/go-common/logger"
	_ "github.com/agentuity/go-common/message"
	_ "github.com/agentuity/go-common/network"
	_ "github.com/agentuity/go-common/request"
	_ "github.com/agentuity/go-common/slice"
	_ "github.com/agentuity/go-common/stream"
	_ "github.com/agentuity/go-common/string"
	_ "github.com/agentuity/go-common/sys"
	_ "github.com/agentuity/go-common/telemetry"
	_ "github.com/agentuity/go-common/tui"
)

func main() {
	fmt.Println("Hi")
}
