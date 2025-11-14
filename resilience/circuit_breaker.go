package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrCircuitBreakerOpen    = errors.New("circuit breaker is open")
	ErrCircuitBreakerTimeout = errors.New("circuit breaker operation timeout")
	ErrTooManyFailures       = errors.New("too many consecutive failures")
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int32

const (
	StateClosed CircuitBreakerState = iota
	StateHalfOpen
	StateOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateHalfOpen:
		return "HALF_OPEN"
	case StateOpen:
		return "OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreakerConfig defines configuration for the circuit breaker
type CircuitBreakerConfig struct {
	// MaxFailures is the maximum number of failures before opening the circuit
	MaxFailures int

	// Timeout is how long to wait before transitioning from Open to Half-Open
	Timeout time.Duration

	// MaxConcurrentRequests is the max requests allowed in Half-Open state
	MaxConcurrentRequests int

	// SuccessThreshold is the number of consecutive successes needed in Half-Open to go to Closed
	SuccessThreshold int

	// RequestTimeout is the maximum time to wait for a single request
	RequestTimeout time.Duration
}

// DefaultCircuitBreakerConfig returns a default configuration
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxFailures:           5,
		Timeout:               30 * time.Second,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      3,
		RequestTimeout:        10 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern for fault tolerance
type CircuitBreaker struct {
	config CircuitBreakerConfig

	// Atomic counters
	state           int32 // CircuitBreakerState
	failures        int32
	successes       int32
	requests        int32
	lastFailureTime int64 // Unix nano

	mu sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		state:  int32(StateClosed),
	}
}

// Execute wraps a function call with circuit breaker logic
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	// Check if we can execute
	if err := cb.beforeRequest(); err != nil {
		return err
	}

	// Execute with timeout
	done := make(chan error, 1)
	go func() {
		defer cb.afterRequest()
		err := fn()
		done <- err
	}()

	// Wait for completion or timeout
	var timeoutCtx context.Context
	var cancel context.CancelFunc

	if cb.config.RequestTimeout > 0 {
		timeoutCtx, cancel = context.WithTimeout(ctx, cb.config.RequestTimeout)
		defer cancel()
	} else {
		timeoutCtx = ctx
	}

	select {
	case err := <-done:
		if err != nil {
			cb.onFailure()
			return err
		}
		cb.onSuccess()
		return nil

	case <-timeoutCtx.Done():
		cb.onFailure()
		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			return ErrCircuitBreakerTimeout
		}
		return timeoutCtx.Err()
	}
}

// beforeRequest checks if the request should be allowed
func (cb *CircuitBreaker) beforeRequest() error {
	state := CircuitBreakerState(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		return nil

	case StateOpen:
		// Check if timeout has elapsed
		if cb.shouldAttemptReset() {
			cb.TransitionToHalfOpen()
			return nil
		}
		return ErrCircuitBreakerOpen

	case StateHalfOpen:
		// Limit concurrent requests in half-open state
		currentRequests := atomic.LoadInt32(&cb.requests)
		if currentRequests >= int32(cb.config.MaxConcurrentRequests) {
			return ErrCircuitBreakerOpen
		}
		atomic.AddInt32(&cb.requests, 1)
		return nil

	default:
		return ErrCircuitBreakerOpen
	}
}

// afterRequest is called after a request completes
func (cb *CircuitBreaker) afterRequest() {
	state := CircuitBreakerState(atomic.LoadInt32(&cb.state))
	if state == StateHalfOpen {
		atomic.AddInt32(&cb.requests, -1)
	}
}

// onSuccess is called when a request succeeds
func (cb *CircuitBreaker) onSuccess() {
	state := CircuitBreakerState(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		// Reset failure counter
		atomic.StoreInt32(&cb.failures, 0)

	case StateHalfOpen:
		successes := atomic.AddInt32(&cb.successes, 1)
		if int(successes) >= cb.config.SuccessThreshold {
			cb.transitionToClosed()
		}
	}
}

// onFailure is called when a request fails
func (cb *CircuitBreaker) onFailure() {
	failures := atomic.AddInt32(&cb.failures, 1)
	atomic.StoreInt64(&cb.lastFailureTime, time.Now().UnixNano())

	state := CircuitBreakerState(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		if int(failures) >= cb.config.MaxFailures {
			cb.transitionToOpen()
		}

	case StateHalfOpen:
		cb.transitionToOpen()
	}
}

// shouldAttemptReset checks if enough time has passed to attempt a reset
func (cb *CircuitBreaker) shouldAttemptReset() bool {
	lastFailure := atomic.LoadInt64(&cb.lastFailureTime)
	return time.Since(time.Unix(0, lastFailure)) >= cb.config.Timeout
}

// transitionToClosed transitions the circuit breaker to closed state
func (cb *CircuitBreaker) transitionToClosed() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.StoreInt32(&cb.state, int32(StateClosed))
	atomic.StoreInt32(&cb.failures, 0)
	atomic.StoreInt32(&cb.successes, 0)
	atomic.StoreInt32(&cb.requests, 0)
}

// transitionToOpen transitions the circuit breaker to open state
func (cb *CircuitBreaker) transitionToOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.StoreInt32(&cb.state, int32(StateOpen))
	atomic.StoreInt64(&cb.lastFailureTime, time.Now().UnixNano())
}

// TransitionToHalfOpen transitions the circuit breaker to half-open state
func (cb *CircuitBreaker) TransitionToHalfOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.StoreInt32(&cb.state, int32(StateHalfOpen))
	atomic.StoreInt32(&cb.successes, 0)
	atomic.StoreInt32(&cb.requests, 0)
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() CircuitBreakerState {
	return CircuitBreakerState(atomic.LoadInt32(&cb.state))
}

// Failures returns the current failure count
func (cb *CircuitBreaker) Failures() int {
	return int(atomic.LoadInt32(&cb.failures))
}

// Successes returns the current success count (only relevant in half-open state)
func (cb *CircuitBreaker) Successes() int {
	return int(atomic.LoadInt32(&cb.successes))
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.transitionToClosed()
}

// Stats returns statistics about the circuit breaker
type CircuitBreakerStats struct {
	State     CircuitBreakerState
	Failures  int
	Successes int
	Requests  int
}

// Stats returns current statistics
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	return CircuitBreakerStats{
		State:     cb.State(),
		Failures:  cb.Failures(),
		Successes: cb.Successes(),
		Requests:  int(atomic.LoadInt32(&cb.requests)),
	}
}
