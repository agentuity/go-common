package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

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

type Client struct {
	ctx           context.Context
	cancel        context.CancelFunc
	logger        logger.Logger
	baseurl       string
	url           string
	authorization string
	handler       Handler
}

// URL returns the ephemeral URL of the bridge connection
func (c *Client) URL() string {
	return c.url
}

// Close closes the bridge client and disconnects from the bridge server
func (c *Client) Close() {
	c.cancel()
}

// Connect connects to the bridge and returns a new Client
func (c *Client) Connect() error {
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	c.url = strings.TrimSpace(string(body))

	c.logger.Debug("created bridge connection to: %s", c.url)
	c.handler.OnConnect(c, c.url)

	go c.run()

	return nil
}

// Reply sends a reply to the incoming request
func (c *Client) Reply(requestId string, status int, headers map[string]string, reader io.Reader) error {
	c.logger.Trace("replying to: %s, status: %d, headers: %s", c.url, status, headers)
	// note: we explicitly do not use a context for the request as we want to ensure the request is sent even if the context is cancelled
	req, err := http.NewRequest("PUT", c.url+"/"+requestId, reader)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", c.authorization)
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
	req, err := http.NewRequestWithContext(c.ctx, "GET", c.url, nil)
	if err != nil {
		c.handler.OnError(c, err)
		return
	}
	req.Header.Set("Authorization", c.authorization)
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
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-c.ctx.Done():
			c.logger.Debug("closed")
			return
		default:
		}
		var data bridgeDataEvent
		buf := scanner.Bytes()
		c.logger.Trace("received: %s", string(buf))
		if err := json.Unmarshal(buf, &data); err != nil {
			c.handler.OnError(c, err)
			return
		}
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
}

// Handler is an interface that defines the callback methods for a bridge handler to implement
type Handler interface {
	// OnConnect is called when the bridge client is connected to the bridge server
	OnConnect(client *Client, url string)

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
	OnConnectFunc func(client *Client, url string)
	OnHeaderFunc  func(client *Client, id string, headers map[string]string)
	OnDataFunc    func(client *Client, id string, data []byte)
	OnCloseFunc   func(client *Client, id string)
	OnErrorFunc   func(client *Client, err error)
}

var _ Handler = (*HandlerCallback)(nil)

func (h *HandlerCallback) OnConnect(client *Client, url string) {
	if h.OnConnectFunc != nil {
		h.OnConnectFunc(client, url)
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
	return &Client{
		ctx:           ctx,
		cancel:        cancel,
		logger:        opts.Logger,
		baseurl:       opts.URL,
		authorization: "Bearer " + opts.APIKey,
		handler:       opts.Handler,
	}
}
