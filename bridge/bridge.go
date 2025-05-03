package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentuity/go-common/logger"
)

type bridgeDataEvent struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

type bridgeHeaderEvent struct {
	bridgeDataEvent
	Headers map[string]string `json:"headers"`
}

type bridgePacketEvent struct {
	bridgeDataEvent
	Data []byte `json:"data"`
}

type bridgeCloseEvent struct {
	bridgeDataEvent
}

type BridgeConnectionInfo struct {
	ExpiresAt    *time.Time `json:"expires_at"`
	WebsocketURL string     `json:"websocket_url"`
	StreamURL    string     `json:"stream_url"`
	ClientURL    string     `json:"client_url"`
	RepliesURL   string     `json:"replies_url"`
	RefreshURL   string     `json:"refresh_url"`
}

// IsExpired returns true if the connection has expired
func (b *BridgeConnectionInfo) IsExpired() bool {
	return b.ExpiresAt != nil && !b.ExpiresAt.IsZero() && time.Now().After(*b.ExpiresAt)
}

type Client struct {
	ctx           context.Context
	cancel        context.CancelFunc
	logger        logger.Logger
	baseurl       string
	expiresAt     *time.Time
	websocketURL  string
	streamURL     string
	clientURL     string
	repliesURL    string
	refreshURL    string
	authorization string
	authLock      sync.RWMutex
	handler       Handler
	once          sync.Once
	wg            sync.WaitGroup
}

// ConnectionInfo returns the connection info for the bridge connection
func (c *Client) ConnectionInfo() BridgeConnectionInfo {
	return BridgeConnectionInfo{
		ExpiresAt:    c.expiresAt,
		WebsocketURL: c.websocketURL,
		StreamURL:    c.streamURL,
		ClientURL:    c.clientURL,
		RepliesURL:   c.repliesURL,
		RefreshURL:   c.refreshURL,
	}
}

// IsExpired returns true if the connection has expired
func (c *Client) IsExpired() bool {
	c.authLock.RLock()
	defer c.authLock.RUnlock()
	if c.expiresAt == nil {
		return true
	}
	if c.expiresAt.IsZero() {
		return true
	}
	expiresAt := c.expiresAt.Add(-1 * time.Minute)
	return time.Now().After(expiresAt)
}

// ExpiresAt returns the expiration time of the connection
func (c *Client) ExpiresAt() time.Time {
	c.authLock.RLock()
	defer c.authLock.RUnlock()
	if c.expiresAt == nil {
		return time.Time{}
	}
	return *c.expiresAt
}

// WebsocketURL returns the websocket URL of the bridge connection
func (c *Client) WebsocketURL() string {
	c.authLock.RLock()
	defer c.authLock.RUnlock()
	return c.websocketURL
}

// StreamURL returns the stream URL of the bridge connection
func (c *Client) StreamURL() string {
	c.authLock.RLock()
	defer c.authLock.RUnlock()
	return c.streamURL
}

// ClientURL returns the emphemeral client URL of the bridge connection
func (c *Client) ClientURL() string {
	c.authLock.RLock()
	defer c.authLock.RUnlock()
	return c.clientURL
}

// Close closes the bridge client and disconnects from the bridge server
func (c *Client) Close() {
	c.once.Do(func() {
		c.cancel()
		c.wg.Wait()
	})
}

// Refresh refreshes the connection to the bridge server
func (c *Client) Refresh() error {
	c.authLock.Lock()
	defer c.authLock.Unlock()
	c.logger.Debug("refreshing connection: %s", c.refreshURL)
	req, err := http.NewRequestWithContext(c.ctx, "POST", c.refreshURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authorization)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error: status code %d", resp.StatusCode)
	}

	var response BridgeConnectionInfo
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return err
	}

	c.expiresAt = response.ExpiresAt
	c.websocketURL = response.WebsocketURL
	c.streamURL = response.StreamURL
	c.clientURL = response.ClientURL
	c.repliesURL = response.RepliesURL
	c.refreshURL = response.RefreshURL
	c.logger.Debug("refreshed bridge connection. websocket: %s, stream: %s, client: %s, replies: %s, expires at: %s", c.websocketURL, c.streamURL, c.clientURL, c.repliesURL, c.expiresAt.Format(time.RFC3339))
	return nil
}

// Connect connects to the bridge and returns a new Client
func (c *Client) Connect() error {
	c.wg.Add(1)
	defer c.wg.Done()
	if c.websocketURL != "" && c.streamURL != "" && c.clientURL != "" && c.repliesURL != "" {
		if c.IsExpired() {
			c.logger.Warn("connection auth token expired, refreshing")
			if err := c.Refresh(); err != nil {
				return err
			}
		} else {
			c.logger.Debug("using provided connection info")
		}
	} else {
		c.logger.Debug("connecting to: %s/bridge", c.baseurl)
		req, err := http.NewRequestWithContext(c.ctx, "POST", c.baseurl+"/bridge", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", c.authorization)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("error: status code %d", resp.StatusCode)
		}

		var response BridgeConnectionInfo
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return err
		}

		c.authLock.Lock()
		defer c.authLock.Unlock()
		c.expiresAt = response.ExpiresAt
		c.websocketURL = response.WebsocketURL
		c.streamURL = response.StreamURL
		c.clientURL = response.ClientURL
		c.repliesURL = response.RepliesURL
		c.refreshURL = response.RefreshURL
	}
	c.logger.Debug("created bridge connection. websocket: %s, stream: %s, client: %s, replies: %s, expires at: %s", c.websocketURL, c.streamURL, c.clientURL, c.repliesURL, c.expiresAt.Format(time.RFC3339))
	c.handler.OnConnect(c)

	go c.run()

	return nil
}

// Reply sends a reply to the incoming request
func (c *Client) Reply(replyId string, status int, headers map[string]string, reader io.Reader) error {
	c.wg.Add(1)
	defer c.wg.Done()
	if c.IsExpired() {
		c.logger.Warn("connection auth token expired, refreshing")
		if err := c.Refresh(); err != nil {
			return fmt.Errorf("error refreshing auth token: %w", err)
		}
	}
	c.authLock.RLock()
	auth := c.authorization
	c.authLock.RUnlock()
	url := strings.Replace(c.repliesURL, "{replyId}", replyId, 1)
	c.logger.Trace("replying to: %s, status: %d, headers: %s", url, status, headers)
	// note: we explicitly do not use a context for the request as we want to ensure the request is sent even if the context is cancelled
	req, err := http.NewRequest("PUT", url, reader)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", auth)
	req.Header.Set("x-bridge-statuscode", strconv.Itoa(status))

	for k, v := range headers {
		req.Header.Set("x-bridge-"+strings.ToLower(k), v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 299 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error: status code %d, body: %s", resp.StatusCode, string(body))
	}

	// just discard the body
	io.Copy(io.Discard, resp.Body)

	return nil
}

func (c *Client) run() {
	c.logger.Debug("starting")
	c.wg.Add(1)
	defer c.wg.Done()
	req, err := http.NewRequestWithContext(c.ctx, "GET", c.streamURL, nil)
	if err != nil {
		c.handler.OnError(c, err)
		return
	}
	c.authLock.RLock()
	auth := c.authorization
	c.authLock.RUnlock()
	req.Header.Set("Authorization", auth)
	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		c.handler.OnError(c, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.handler.OnError(c, fmt.Errorf("error: status code %d, body: %s", resp.StatusCode, string(body)))
		return
	}
	c.logger.Debug("connected")
	defer func() {
		c.logger.Debug("disconnected")
		c.handler.OnDisconnect(c)
	}()
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if c.IsExpired() {
					if err := c.Refresh(); err != nil {
						c.logger.Error("error refreshing auth token: %s", err)
					}
				}
			case <-c.ctx.Done():
				return
			}
		}
	}()
	reader := make(chan []byte, 10) // the bigger the buffer the more memory we use but it will reduce backpressure on the server
	go func() {
		for buf := range reader {
			var data bridgeDataEvent
			if err := json.Unmarshal(buf, &data); err != nil {
				c.handler.OnError(c, err)
				return
			}
			c.logger.Debug("[%s] action: %s", data.ID, data.Action)
			switch data.Action {
			case "close":
				c.handler.OnClose(c, data.ID)
			case "header":
				var header bridgeHeaderEvent
				if err := json.Unmarshal(buf, &header); err != nil {
					c.handler.OnError(c, err)
					return
				}
				c.handler.OnHeader(c, data.ID, header.Headers)
			case "data":
				var packet bridgePacketEvent
				if err := json.Unmarshal(buf, &packet); err != nil {
					c.handler.OnError(c, err)
					return
				}
				c.handler.OnData(c, data.ID, packet.Data)
			}
		}
	}()
	headerBuffer := make([]byte, 6) // eof + payload length
	var pendingBuffer bytes.Buffer
	var cancelled bool
	for !cancelled {
		hn, err := resp.Body.Read(headerBuffer)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "unexpected EOF") {
				cancelled = true
				continue
			}
			c.handler.OnError(c, err)
			continue
		}
		if hn != 6 {
			c.handler.OnError(c, fmt.Errorf("expected 6 bytes for header, got %d", hn))
			return
		}
		var eof bool
		if headerBuffer[0] == '1' {
			eof = true
		}
		payloadLength, err := strconv.Atoi(string(headerBuffer[1:])) // only 999999 is supported by realistically on 65336
		if err != nil {
			c.handler.OnError(c, err)
			return
		}
		buf := make([]byte, payloadLength)
		pn, err := resp.Body.Read(buf)
		if err == nil && pn != payloadLength {
			c.handler.OnError(c, fmt.Errorf("expected %d bytes for payload, got %d", payloadLength, pn))
			return
		} else if err == nil {
			pendingBuffer.Write(buf)
			if eof {
				buf := pendingBuffer.Bytes()
				// we must copy the buffer as it will be reused and its not safe to access it after the read
				newbuf := make([]byte, len(buf))
				copy(newbuf, buf)
				pendingBuffer.Reset()
				reader <- newbuf
			}
		}
		if err != nil {
			if err == io.EOF || err == context.Canceled {
				cancelled = true
				continue
			}
			c.handler.OnError(c, err)
			return
		}
	}
	close(reader)
	c.logger.Debug("reader closed")
}

// Handler is an interface that defines the callback methods for a bridge handler to implement
type Handler interface {
	// OnConnect is called when the bridge client is connected to the bridge server
	OnConnect(client *Client)

	// OnDisconnect is called when the bridge client is disconnected from the bridge server
	OnDisconnect(client *Client)

	// OnHeader is called when a header is received from the bridge. this will only be called once before any data is sent.
	OnHeader(client *Client, id string, headers map[string]string)

	// OnData is called when a data is received from the bridge. this will be called multiple times if the data is large.
	OnData(client *Client, id string, data []byte)

	// OnClose is called when the bridge request is completed and no more data will be sent
	OnClose(client *Client, id string)

	// OnError is called when an error occurs at any point in the bridge client
	OnError(client *Client, err error)
}

// HandlerCallback is a struct that implements the BridgeHandler interface
type HandlerCallback struct {
	OnConnectFunc    func(client *Client)
	OnDisconnectFunc func(client *Client)
	OnHeaderFunc     func(client *Client, id string, headers map[string]string)
	OnDataFunc       func(client *Client, id string, data []byte)
	OnCloseFunc      func(client *Client, id string)
	OnErrorFunc      func(client *Client, err error)
}

var _ Handler = (*HandlerCallback)(nil)

func (h *HandlerCallback) OnConnect(client *Client) {
	if h.OnConnectFunc != nil {
		h.OnConnectFunc(client)
	}
}

func (h *HandlerCallback) OnDisconnect(client *Client) {
	if h.OnDisconnectFunc != nil {
		h.OnDisconnectFunc(client)
	}
}

func (h *HandlerCallback) OnHeader(client *Client, id string, headers map[string]string) {
	if h.OnHeaderFunc != nil {
		h.OnHeaderFunc(client, id, headers)
	}
}

func (h *HandlerCallback) OnData(client *Client, id string, data []byte) {
	if h.OnDataFunc != nil {
		h.OnDataFunc(client, id, data)
	}
}

func (h *HandlerCallback) OnClose(client *Client, id string) {
	if h.OnCloseFunc != nil {
		h.OnCloseFunc(client, id)
	}
}

func (h *HandlerCallback) OnError(client *Client, err error) {
	if h.OnErrorFunc != nil {
		h.OnErrorFunc(client, err)
	}
}

type Options struct {
	// Context is the context for the bridge client (optional)
	Context context.Context
	// Logger is the logger for the bridge client (optional)
	Logger logger.Logger
	// URL is the URL of the bridge (optional)
	URL string
	// APIKey is the API key for the bridge (optional, will use AGENTUITY_API_KEY environment variable if not set)
	APIKey string
	// Handler is the handler for the bridge client (required)
	Handler Handler
	// ConnectionInfo is the connection info for the bridge client (optional) which can be used to pre-populate the connection info
	ConnectionInfo *BridgeConnectionInfo
}

// New creates a new BridgeClient
func New(opts Options) *Client {
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	if opts.Logger == nil {
		opts.Logger = logger.NewConsoleLogger()
	}
	if opts.URL == "" {
		opts.URL = "https://agentuity.ai/"
	}
	if opts.APIKey == "" {
		opts.APIKey = os.Getenv("AGENTUITY_API_KEY")
		if opts.APIKey == "" {
			opts.Logger.Fatal("Neither the APIKey option and AGENTUITY_API_KEY environment variable are set and are required")
		}
	}
	if opts.Handler == nil {
		opts.Logger.Fatal("Handler option is required")
	}
	ctx, cancel := context.WithCancel(opts.Context)
	client := &Client{
		ctx:           ctx,
		cancel:        cancel,
		logger:        opts.Logger,
		baseurl:       opts.URL,
		authorization: "Bearer " + opts.APIKey,
		handler:       opts.Handler,
	}
	if opts.ConnectionInfo != nil {
		client.expiresAt = opts.ConnectionInfo.ExpiresAt
		client.websocketURL = opts.ConnectionInfo.WebsocketURL
		client.streamURL = opts.ConnectionInfo.StreamURL
		client.clientURL = opts.ConnectionInfo.ClientURL
		client.repliesURL = opts.ConnectionInfo.RepliesURL
		client.refreshURL = opts.ConnectionInfo.RefreshURL
	}
	return client
}
