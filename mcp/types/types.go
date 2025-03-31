package types

import (
	"encoding/json"
)

type ContentType string

const (
	ContentTypeText  ContentType = "text"
	ContentTypeImage ContentType = "image"
	ContentTypeAudio ContentType = "audio"
)

type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type ToolAnnotation struct {
	ReadOnly    bool `json:"readOnly,omitempty"`
	Destructive bool `json:"destructive,omitempty"`
}

type Content struct {
	Type  ContentType `json:"type"`
	Text  string      `json:"text,omitempty"`
	Image *Image      `json:"image,omitempty"`
	Audio *Audio      `json:"audio,omitempty"`
}

type Image struct {
	URL      string `json:"url,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

type Audio struct {
	URL      string `json:"url,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

type ToolResponse struct {
	Content *Content `json:"content,omitempty"`
}

type ProgressNotification struct {
	Progress      float64     `json:"progress"`
	Message       string      `json:"message,omitempty"`
	ID            interface{} `json:"id,omitempty"`
	ProgressToken string      `json:"progressToken,omitempty"`
}
