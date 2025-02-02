package request

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	ghttp "net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/agentuity/go-common/dns"
	cstr "github.com/agentuity/go-common/string"
	"golang.org/x/sync/semaphore"
)

var ErrTooManyAttempts = errors.New("too many attempts")

const (
	userAgentHeaderValue = "Agentuity (+https://agentuity.com)"
)

// Request is an interface for an HTTP request.
type Request interface {
	// Method returns the HTTP method.
	Method() string
	// URL returns the URL.
	URL() string
	// Headers returns the headers.
	Headers() map[string]string
	// Payload returns the payload.
	Payload() []byte
}

type HTTPRequest struct {
	method  string
	url     string
	headers map[string]string
	payload []byte
}

func (r *HTTPRequest) Method() string {
	return r.method
}

func (r *HTTPRequest) URL() string {
	return r.url
}

func (r *HTTPRequest) Headers() map[string]string {
	return r.headers
}

func (r *HTTPRequest) Payload() []byte {
	return r.payload
}

// NewHTTPRequest creates a new HTTPRequest that implements the Request interface.
func NewHTTPRequest(method string, url string, headers map[string]string, payload []byte) Request {
	return &HTTPRequest{method, url, headers, payload}
}

// NewHTTPGetRequest creates a new HTTPRequest that implements the Request interface for GET requests.
func NewHTTPGetRequest(url string, headers map[string]string) Request {
	return &HTTPRequest{ghttp.MethodGet, url, headers, nil}
}

// NewHTTPPostRequest creates a new HTTPRequest that implements the Request interface for POST requests.
func NewHTTPPostRequest(url string, headers map[string]string, payload []byte) Request {
	return &HTTPRequest{ghttp.MethodPost, url, headers, payload}
}

// Response is the response from an HTTP request.
type Response struct {
	StatusCode int               `json:"statusCode"`
	Body       []byte            `json:"body,omitempty"`
	Headers    map[string]string `json:"headers"`
	Attempts   uint              `json:"attempts"`
	Latency    time.Duration     `json:"latency"`
}

// Recorder is an interface for recording request / responses.
type Recorder interface {
	OnResponse(ctx context.Context, req Request, resp *Response)
}

// Http is an interface for making HTTP requests.
type Http interface {
	// Deliver sends a request and returns a response.
	Deliver(ctx context.Context, request Request) (*Response, error)
}

// RetryBackoff is an interface for retrying a request with a backoff.
type RetryBackoff interface {
	// BackOff returns the duration to wait before retrying.
	BackOff(attempt uint) time.Duration
}

type powerOfTwoBackoff struct {
	min time.Duration
	max time.Duration
}

func (p *powerOfTwoBackoff) BackOff(attempt uint) time.Duration {
	if attempt == 0 {
		return p.min
	}
	ms := time.Duration(math.Pow(2, float64(attempt))) * p.min
	if ms > p.max {
		return p.max
	}
	return ms
}

// NewMinMaxBackoff creates a new RetryBackoff that retries with a backoff of min * 2^attempt.
func NewMinMaxBackoff(min time.Duration, max time.Duration) RetryBackoff {
	return &powerOfTwoBackoff{min, max}
}

type http struct {
	transport   *ghttp.Transport
	timeout     time.Duration // set for testing but defaults to 55 seconds otherwise
	dur         time.Duration // set for testing but defaults to 1 second otherwise
	recorder    Recorder
	count       uint64
	semaphore   *semaphore.Weighted
	maxAttempts uint
	backoff     RetryBackoff
}

var _ Http = (*http)(nil)

func (h *http) shouldRetry(resp *ghttp.Response, err error) bool {
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "connection reset") || strings.Contains(msg, "connection refused") || strings.Contains(msg, "EOF") {
			return true
		}
	}
	if resp != nil {
		switch resp.StatusCode {
		case ghttp.StatusRequestTimeout, ghttp.StatusBadGateway, ghttp.StatusServiceUnavailable, ghttp.StatusGatewayTimeout, ghttp.StatusTooManyRequests:
			return true
		}
	}
	return false
}

func (h *http) toResponse(resp *ghttp.Response, attempt uint, latency time.Duration) (*Response, error) {
	if resp == nil {
		return nil, nil
	}
	var body []byte
	if resp.Body != nil {
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		body = b
	}
	headers := make(map[string]string)
	for k, v := range resp.Header {
		headers[k] = strings.Join(v, ", ")
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Body:       body,
		Headers:    headers,
		Attempts:   attempt,
		Latency:    latency,
	}, nil
}

var isNumber = regexp.MustCompile("^[0-9]+$")

func (h *http) generateRequestId(req Request) string {
	count := atomic.AddUint64(&h.count, 1)
	return fmt.Sprintf("%d/%s", count, cstr.NewHash(req.URL(), req.Payload(), time.Now().UnixNano()))
}

func (h *http) Deliver(ctx context.Context, req Request) (*Response, error) {
	started := time.Now()
	if err := h.semaphore.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("error acquiring semaphore: %w", err)
	}
	defer h.semaphore.Release(1)
	var attempt uint
	var resp *ghttp.Response
	var response *Response
	var c context.Context
	var cancel context.CancelFunc
	maxAttempts := h.maxAttempts
	if maxAttempts == 1 {
		// for testing we want to make sure we don't wait too long
		c, cancel = context.WithTimeout(ctx, time.Second*3)
		// we only want to try once for tests
	} else {
		c, cancel = context.WithTimeout(ctx, h.timeout)
	}
	defer cancel()
	headers := req.Headers()
	if headers == nil {
		headers = make(map[string]string)
	}
	for attempt < maxAttempts {
		attempt++
		reqId := h.generateRequestId(req)
		var body io.Reader
		payload := req.Payload()
		if len(payload) > 0 {
			body = bytes.NewBuffer(payload)
		}
		hreq, err := ghttp.NewRequestWithContext(c, req.Method(), req.URL(), body)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			hreq.Header.Set(k, v)
		}
		hreq.Header.Set("User-Agent", userAgentHeaderValue)
		hreq.Header.Set("X-Request-Id", reqId)
		hreq.Header.Set("X-Attempt", strconv.Itoa(int(attempt)))
		resp, err = h.transport.RoundTrip(hreq)
		if err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
			return nil, err
		}
		if h.recorder != nil && resp != nil /*&& !req.TestOnly*/ {
			r, err := h.toResponse(resp, attempt, time.Since(started))
			if err != nil {
				return nil, fmt.Errorf("error converting response: %w", err)
			}
			// update our headers
			headers["User-Agent"] = userAgentHeaderValue
			headers["X-Request-Id"] = reqId
			headers["X-Attempt"] = strconv.Itoa(int(attempt))
			h.recorder.OnResponse(ctx, req, r)
			response = r // set it so we don't try and re-read the body again
		}
		if h.shouldRetry(resp, err) /*&& !req.TestOnly*/ {
			select {
			case <-ctx.Done():
				return nil, context.Canceled
			default:
			}
			if attempt == maxAttempts {
				// don't worry about sleeping and reading the body if we're not going to retry
				break
			}
			ms := h.dur * time.Duration(attempt)
			if h.backoff != nil {
				ms = h.backoff.BackOff(attempt)
			}
			if resp != nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				ra := resp.Header.Get("Retry-After")
				if ra != "" {
					if isNumber.MatchString(ra) {
						afterSeconds, _ := strconv.Atoi(ra)
						if afterSeconds > 0 {
							ms = time.Second * time.Duration(afterSeconds)
						}
					} else {
						if tv, err := time.Parse(ghttp.TimeFormat, ra); err == nil {
							ms = time.Until(tv)
						}
					}
				}
			}
			if ms > 0 {
				time.Sleep(ms)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if response == nil {
			return h.toResponse(resp, attempt, time.Since(started))
		}
		return response, nil
	}
	if response == nil && resp != nil {
		r, err := h.toResponse(resp, attempt, time.Since(started))
		if err != nil {
			return nil, err
		}
		return r, ErrTooManyAttempts
	}
	return response, ErrTooManyAttempts
}

type configOpts struct {
	recorder    Recorder
	max         uint64
	dns         dns.DNS
	timeout     time.Duration
	dur         time.Duration
	maxAttempts uint
	backoff     RetryBackoff
}

type ConfigOpt func(opts *configOpts)

// New returns a new HTTP implementation.
func New(opts ...ConfigOpt) Http {
	var c configOpts
	c.timeout = time.Second * 55
	c.dur = time.Second
	c.max = 100
	c.maxAttempts = 4
	c.backoff = NewMinMaxBackoff(time.Millisecond*50, time.Second*10)
	for _, opt := range opts {
		opt(&c)
	}
	if c.max <= 0 {
		panic("max was nil")
	}
	if c.maxAttempts <= 0 {
		panic("maxAttempts was nil")
	}
	var transport *ghttp.Transport
	if c.dns != nil {
		transport = &ghttp.Transport{
			DialContext: func(ctx context.Context, network string, addr string) (conn net.Conn, err error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ok, ip, err := c.dns.Lookup(ctx, host)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, fmt.Errorf("dns lookup failed: couldn't find ip for %s", host)
				}
				var dialer net.Dialer
				conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				return
			},
		}
	} else {
		transport = ghttp.DefaultTransport.(*ghttp.Transport)
	}
	return &http{
		transport:   transport,
		timeout:     c.timeout,
		dur:         c.dur,
		recorder:    c.recorder,
		semaphore:   semaphore.NewWeighted(int64(c.max)),
		maxAttempts: c.maxAttempts,
		backoff:     c.backoff,
	}
}

// WithDNS sets the dns resolver for the http client.
func WithDNS(dns dns.DNS) ConfigOpt {
	return func(opts *configOpts) {
		opts.dns = dns
	}
}

// WithRecorder sets the recorder for the http client.
func WithRecorder(recorder Recorder) ConfigOpt {
	return func(opts *configOpts) {
		opts.recorder = recorder
	}
}

// WithMaxConcurrency sets the max number of concurrent requests.
func WithMaxConcurrency(max uint64) ConfigOpt {
	return func(opts *configOpts) {
		opts.max = max
	}
}

// WithTimeout sets the timeout for the http client.
func WithTimeout(timeout time.Duration) ConfigOpt {
	return func(opts *configOpts) {
		opts.timeout = timeout
	}
}

// WithBackoffDuration sets the backoff duration for the http client.
func WithBackoffDuration(dur time.Duration) ConfigOpt {
	return func(opts *configOpts) {
		opts.dur = dur
	}
}

// WithMaxAttempts sets the max number of attempts for the http client.
func WithMaxAttempts(max uint) ConfigOpt {
	return func(opts *configOpts) {
		opts.maxAttempts = max
	}
}

// WithBackoff sets the backoff strategy for the http client.
func WithBackoff(backoff RetryBackoff) ConfigOpt {
	return func(opts *configOpts) {
		opts.backoff = backoff
	}
}
