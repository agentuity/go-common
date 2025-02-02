package request

import (
	"context"
	"fmt"
	ghttp "net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/semaphore"
)

func TestHTTPOK(t *testing.T) {
	h := New()
	srv := httptest.NewServer(ghttp.HandlerFunc(func(w ghttp.ResponseWriter, r *ghttp.Request) {
		assert.Contains(t, r.Header, "User-Agent")
		assert.Contains(t, r.Header, "X-Request-Id")
		assert.Equal(t, "1", r.Header.Get("X-Attempt"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ghttp.StatusOK)
		w.Write([]byte(fmt.Sprintf(`{"message":"%s"}`, r.Method)))
	}))
	defer srv.Close()
	resp, err := h.Deliver(context.Background(), NewHTTPPostRequest(srv.URL, map[string]string{}, []byte("hello")))
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, ghttp.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"message":"POST"}`, string(resp.Body))
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	assert.Equal(t, uint(1), resp.Attempts)
	resp, err = h.Deliver(context.Background(), NewHTTPGetRequest(srv.URL, map[string]string{}))
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, ghttp.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"message":"GET"}`, string(resp.Body))
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	assert.Equal(t, uint(1), resp.Attempts)
}

func TestHTTPRetry(t *testing.T) {
	h := New()
	var count int
	srv := httptest.NewServer(ghttp.HandlerFunc(func(w ghttp.ResponseWriter, r *ghttp.Request) {
		count++
		assert.Contains(t, r.Header, "User-Agent")
		assert.Contains(t, r.Header, "X-Request-Id")
		assert.Equal(t, strconv.Itoa(count), r.Header.Get("X-Attempt"))
		if count < 3 {
			w.WriteHeader(ghttp.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ghttp.StatusOK)
		w.Write([]byte(`{"message":"hello"}`))
	}))
	defer srv.Close()
	resp, err := h.Deliver(context.Background(), NewHTTPRequest(ghttp.MethodPost, srv.URL, map[string]string{}, nil))
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, ghttp.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"message":"hello"}`, string(resp.Body))
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	assert.Equal(t, 3, count)
	assert.Equal(t, uint(3), resp.Attempts)
}

func TestHTTPRetryWithRetryAfterHeader(t *testing.T) {
	h := New(WithMaxAttempts(3))
	var count int
	ts := time.Now()
	srv := httptest.NewServer(ghttp.HandlerFunc(func(w ghttp.ResponseWriter, r *ghttp.Request) {
		count++
		if count < 2 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(ghttp.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ghttp.StatusOK)
		w.Write([]byte(`{"message":"hello"}`))
	}))
	defer srv.Close()
	resp, err := h.Deliver(context.Background(), NewHTTPRequest(ghttp.MethodPost, srv.URL, map[string]string{}, nil))
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, ghttp.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"message":"hello"}`, string(resp.Body))
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	assert.Equal(t, 2, count)
	assert.True(t, time.Since(ts) > 2*time.Second)
	assert.Equal(t, uint(2), resp.Attempts)
	assert.True(t, resp.Latency > 2*time.Second)
}

func TestHTTPRetryWithRetryAfterHeaderAsTime(t *testing.T) {
	h := New()
	var count int
	srv := httptest.NewServer(ghttp.HandlerFunc(func(w ghttp.ResponseWriter, r *ghttp.Request) {
		count++
		if count < 2 {
			w.Header().Set("Retry-After", time.Now().Add(3*time.Second).UTC().Format(ghttp.TimeFormat))
			w.WriteHeader(ghttp.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ghttp.StatusOK)
		w.Write([]byte(`{"message":"hello"}`))
	}))
	defer srv.Close()
	resp, err := h.Deliver(context.Background(), NewHTTPRequest(ghttp.MethodPost, srv.URL, map[string]string{}, nil))
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, ghttp.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"message":"hello"}`, string(resp.Body))
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	assert.Equal(t, 2, count)
	assert.Equal(t, uint(2), resp.Attempts)
	assert.True(t, resp.Latency > 2*time.Second)
}

func TestHTTPRetryTimeout(t *testing.T) {
	var h http
	h.dur = 1 * time.Millisecond
	h.timeout = time.Second
	h.semaphore = semaphore.NewWeighted(1)
	h.transport = &ghttp.Transport{}
	h.maxAttempts = 3
	var count int
	srv := httptest.NewServer(ghttp.HandlerFunc(func(w ghttp.ResponseWriter, r *ghttp.Request) {
		count++
		w.WriteHeader(ghttp.StatusBadGateway)
	}))
	defer srv.Close()
	resp, err := h.Deliver(context.Background(), NewHTTPRequest(ghttp.MethodPost, srv.URL, map[string]string{}, nil))
	assert.Error(t, err, ErrTooManyAttempts)
	assert.NotNil(t, resp)
	assert.Equal(t, ghttp.StatusBadGateway, resp.StatusCode)
	assert.Equal(t, uint(3), resp.Attempts)
	assert.True(t, resp.Latency > 0)
}

type testRecord struct {
	req  Request
	resp *Response
}

var _ Recorder = (*testRecord)(nil)

func (r *testRecord) OnResponse(ctx context.Context, req Request, resp *Response) {
	r.req = req
	r.resp = resp
}

func TestHTTPRecorder(t *testing.T) {
	var h http
	var tr testRecord
	h.recorder = &tr
	h.dur = 1 * time.Millisecond
	h.timeout = time.Second
	h.semaphore = semaphore.NewWeighted(1)
	h.transport = &ghttp.Transport{}
	h.maxAttempts = 3
	var count int
	srv := httptest.NewServer(ghttp.HandlerFunc(func(w ghttp.ResponseWriter, r *ghttp.Request) {
		count++
		w.WriteHeader(ghttp.StatusBadGateway)
	}))
	defer srv.Close()
	resp, err := h.Deliver(context.Background(), NewHTTPRequest(ghttp.MethodPost, srv.URL, map[string]string{}, nil))
	assert.Error(t, err, ErrTooManyAttempts)
	assert.NotNil(t, resp)
	assert.Equal(t, ghttp.StatusBadGateway, resp.StatusCode)
	assert.NotNil(t, tr.req)
	assert.NotNil(t, tr.resp)
	assert.Equal(t, srv.URL, tr.req.URL())
	assert.Equal(t, ghttp.StatusBadGateway, tr.resp.StatusCode)
}

func TestHTTPTimeout(t *testing.T) {
	var h http
	h.timeout = time.Millisecond * 500
	h.dur = 1 * time.Millisecond
	h.semaphore = semaphore.NewWeighted(1)
	h.transport = &ghttp.Transport{}
	h.maxAttempts = 3
	srv := httptest.NewServer(ghttp.HandlerFunc(func(w ghttp.ResponseWriter, r *ghttp.Request) {
		time.Sleep(time.Second)
		w.WriteHeader(ghttp.StatusOK)
	}))
	defer srv.Close()
	resp, err := h.Deliver(context.Background(), NewHTTPRequest(ghttp.MethodPost, srv.URL, map[string]string{}, nil))
	assert.Error(t, err, context.DeadlineExceeded)
	assert.Nil(t, resp)
}

func TestHTTPMaxAttempts(t *testing.T) {
	var h http
	var tr testRecord
	h.recorder = &tr
	h.dur = 1 * time.Millisecond
	h.timeout = time.Second
	h.semaphore = semaphore.NewWeighted(1)
	h.transport = &ghttp.Transport{}
	h.maxAttempts = 1
	var count int
	srv := httptest.NewServer(ghttp.HandlerFunc(func(w ghttp.ResponseWriter, r *ghttp.Request) {
		count++
		w.WriteHeader(ghttp.StatusBadGateway)
	}))
	defer srv.Close()
	resp, err := h.Deliver(context.Background(), NewHTTPGetRequest(srv.URL, nil))
	assert.Error(t, err, ErrTooManyAttempts)
	assert.NotNil(t, resp)
	assert.Equal(t, ghttp.StatusBadGateway, resp.StatusCode)
	assert.NotNil(t, tr.req)
	assert.NotNil(t, tr.resp)
	assert.Equal(t, srv.URL, tr.req.URL())
	assert.Equal(t, ghttp.StatusBadGateway, tr.resp.StatusCode)
	assert.Equal(t, uint(1), tr.resp.Attempts)
}

type testBackoff struct {
	count uint
}

func (t *testBackoff) BackOff(attempt uint) time.Duration {
	t.count++
	return time.Millisecond
}

func TestHTTPBackoff(t *testing.T) {
	var h http
	var tr testRecord
	var tb testBackoff
	h.recorder = &tr
	h.dur = 1 * time.Millisecond
	h.timeout = time.Second
	h.semaphore = semaphore.NewWeighted(1)
	h.transport = &ghttp.Transport{}
	h.maxAttempts = 3
	h.backoff = &tb
	var count int
	srv := httptest.NewServer(ghttp.HandlerFunc(func(w ghttp.ResponseWriter, r *ghttp.Request) {
		count++
		w.WriteHeader(ghttp.StatusBadGateway)
	}))
	defer srv.Close()
	resp, err := h.Deliver(context.Background(), NewHTTPGetRequest(srv.URL, nil))
	assert.Error(t, err, ErrTooManyAttempts)
	assert.NotNil(t, resp)
	assert.Equal(t, ghttp.StatusBadGateway, resp.StatusCode)
	assert.NotNil(t, tr.req)
	assert.NotNil(t, tr.resp)
	assert.Equal(t, srv.URL, tr.req.URL())
	assert.Equal(t, ghttp.StatusBadGateway, tr.resp.StatusCode)
	assert.Equal(t, uint(3), tr.resp.Attempts)
	assert.Equal(t, uint(2), tb.count)
}
