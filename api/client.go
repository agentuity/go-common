package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/agentuity/go-common/logger"
	"github.com/agentuity/go-common/sys"
)

var (
	Version = "dev"
	Commit  = "unknown"
	retry   = 5
)

type Client struct {
	ctx     context.Context
	baseURL string
	token   string
	client  *http.Client
	logger  logger.Logger
}

type Error struct {
	URL      string
	Method   string
	Status   int
	Body     string
	TheError error
	TraceID  string
}

func (e *Error) Error() string {
	if e == nil || e.TheError == nil {
		return ""
	}
	return e.TheError.Error()
}

func NewError(url, method string, status int, body string, err error, traceID string) *Error {
	return &Error{
		URL:      url,
		Method:   method,
		Status:   status,
		Body:     body,
		TheError: err,
		TraceID:  traceID,
	}
}

func New(ctx context.Context, logger logger.Logger, baseURL, token string) *Client {
	return &Client{
		ctx:     ctx,
		logger:  logger,
		baseURL: baseURL,
		token:   token,
		client:  http.DefaultClient,
	}
}

type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	Error   struct {
		Issues []struct {
			Code    string   `json:"code"`
			Message string   `json:"message"`
			Path    []string `json:"path"`
		} `json:"issues"`
	} `json:"error"`
}

func UserAgent() string {
	gitSHA := Commit
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				gitSHA = setting.Value
			}
		}
	}
	return "Agentuity API Client/" + Version + " (" + gitSHA + ")"
}

func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
			return true
		} else if msg := err.Error(); strings.Contains(msg, "EOF") {
			return true
		}
	}
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusRequestTimeout, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusTooManyRequests:
			return true
		}
	}
	return false
}

// safeBodyPreview returns a safe preview of the response body for logging,
// preventing PII exposure by checking content-type and truncating or redacting sensitive data.
func safeBodyPreview(body []byte, contentType string, maxChars int) string {
	if maxChars == 0 {
		maxChars = 200
	}

	// Check if the content is binary by examining content-type
	isBinary := false
	lowerContentType := strings.ToLower(contentType)

	// Binary types that should not be logged as text
	binaryTypes := []string{
		"image/", "video/", "audio/", "application/octet-stream",
		"application/pdf", "application/zip", "application/gzip",
		"application/x-tar", "application/x-rar", "font/",
	}

	for _, binaryType := range binaryTypes {
		if strings.Contains(lowerContentType, binaryType) {
			isBinary = true
			break
		}
	}

	// For binary content, return hash and size instead
	if isBinary {
		hash := sha256.Sum256(body)
		return fmt.Sprintf("<binary: %d bytes, sha256=%s>", len(body), hex.EncodeToString(hash[:8]))
	}

	// Safe text types that can be logged (with truncation)
	safeTextTypes := []string{
		"text/", "application/json", "application/xml",
		"application/javascript", "application/x-www-form-urlencoded",
	}

	isSafeText := false
	for _, safeType := range safeTextTypes {
		if strings.Contains(lowerContentType, safeType) {
			isSafeText = true
			break
		}
	}

	// If content-type is unknown or unsafe, treat cautiously
	if !isSafeText && contentType != "" {
		hash := sha256.Sum256(body)
		return fmt.Sprintf("<unknown type: %d bytes, sha256=%s>", len(body), hex.EncodeToString(hash[:8]))
	}

	// Convert body to string
	bodyStr := string(body)

	// Truncate if necessary
	if len(bodyStr) > maxChars {
		return bodyStr[:maxChars] + "[truncated, total: " + fmt.Sprintf("%d", len(bodyStr)) + " chars]"
	}

	return bodyStr
}

func (c *Client) Do(method, pathParam string, payload any, response any) error {
	var traceID string

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return NewError(c.baseURL, method, 0, "", fmt.Errorf("error parsing base url: %w", err), traceID)
	}

	i := strings.Index(pathParam, "?")
	if i != -1 {
		u.RawQuery = pathParam[i+1:]
		pathParam = pathParam[:i]
	}

	basePath := u.Path
	if pathParam == "" {
		u.Path = basePath
	} else if basePath == "" || basePath == "/" {
		u.Path = pathParam
	} else {
		u.Path = path.Join(basePath, pathParam)
	}
	var body []byte
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return NewError(u.String(), method, 0, "", fmt.Errorf("error marshalling payload: %w", err), traceID)
		}
	}
	c.logger.Trace("sending request: %s %s", method, u.String())

	req, err := http.NewRequestWithContext(c.ctx, method, u.String(), bytes.NewBuffer(body))
	if err != nil {
		return NewError(u.String(), method, 0, "", fmt.Errorf("error creating request: %w", err), traceID)
	}

	req.Header.Set("User-Agent", UserAgent())
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	var resp *http.Response
	for i := range retry {
		isLast := i == retry-1
		var err error
		resp, err = c.client.Do(req)
		if shouldRetry(resp, err) && !isLast {
			c.logger.Trace("client returned retryable error, retrying...")
			if resp != nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			if payload != nil {
				req.Body = io.NopCloser(bytes.NewReader(body))
				req.GetBody = func() (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader(body)), nil
				}
			}
			// exponential backoff
			v := 150 * math.Pow(2, float64(i))
			time.Sleep(time.Duration(v) * time.Millisecond)
			continue
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return NewError(u.String(), method, 0, "", fmt.Errorf("error sending request: %w", err), traceID)
		}
		break
	}
	defer resp.Body.Close()
	c.logger.Debug("response status: %s", resp.Status)

	if resp.Header != nil {
		traceID = resp.Header.Get("traceparent")
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return NewError(u.String(), method, 0, "", fmt.Errorf("error reading response body: %w", err), traceID)
	}

	// Log safe preview of response body to avoid PII exposure
	contentType := resp.Header.Get("content-type")
	safePreview := safeBodyPreview(respBody, contentType, 200)
	c.logger.Debug("response body: %s, content-type: %s, user-agent: %s", safePreview, contentType, UserAgent())
	if resp.StatusCode > 299 && strings.Contains(resp.Header.Get("content-type"), "application/json") {
		var apiResponse Response
		if err := json.Unmarshal(respBody, &apiResponse); err != nil {
			return NewError(u.String(), method, resp.StatusCode, string(respBody), fmt.Errorf("error unmarshalling response: %w", err), traceID)
		}
		if !apiResponse.Success {
			if apiResponse.Code == "UPGRADE_REQUIRED" {
				return NewError(u.String(), method, http.StatusUnprocessableEntity, apiResponse.Message, fmt.Errorf("%s", apiResponse.Message), traceID)
			}
			if len(apiResponse.Error.Issues) > 0 {
				var errs []string
				for _, issue := range apiResponse.Error.Issues {
					msg := fmt.Sprintf("%s (%s)", issue.Message, issue.Code)
					if issue.Path != nil {
						msg = msg + " " + strings.Join(issue.Path, ".")
					}
					errs = append(errs, msg)
				}
				return NewError(u.String(), method, resp.StatusCode, string(respBody), fmt.Errorf("%s", strings.Join(errs, ". ")), traceID)
			}
			if apiResponse.Message != "" {
				return NewError(u.String(), method, resp.StatusCode, string(respBody), fmt.Errorf("%s", apiResponse.Message), traceID)
			}
		}
	}

	if resp.StatusCode > 299 {
		return NewError(u.String(), method, resp.StatusCode, string(respBody), fmt.Errorf("request failed with status (%s)", resp.Status), traceID)
	}

	if response != nil {
		if err := json.Unmarshal(respBody, &response); err != nil {
			return NewError(u.String(), method, resp.StatusCode, string(respBody), fmt.Errorf("error JSON decoding response: %w", err), traceID)
		}
	}
	return nil
}

var testInside bool

func TransformUrl(urlString string) string {
	// NOTE: these urls are special cases for local development inside a container
	if strings.Contains(urlString, "api.agentuity.dev") || strings.Contains(urlString, "localhost:") || strings.Contains(urlString, "127.0.0.1:") {
		if sys.IsRunningInsideDocker() || testInside {
			if strings.HasPrefix(urlString, "https://api.agentuity.dev") {
				u, _ := url.Parse(urlString)
				u.Scheme = "http"
				u.Host = "host.docker.agentuity.io:3012"
				return u.String()
			}
			port := regexp.MustCompile(`:(\d+)`)
			host := "host.docker.agentuity.io:3000"
			if port.MatchString(urlString) {
				host = "host.docker.agentuity.io:" + port.FindStringSubmatch(urlString)[1]
			}
			u, _ := url.Parse(urlString)
			u.Scheme = "http"
			u.Host = host
			return u.String()
		}
	}
	return urlString
}

type CLIUrls struct {
	API       string
	App       string
	Transport string
	Gravity   string
}
