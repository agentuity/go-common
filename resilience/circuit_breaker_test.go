package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	if cb.State() != StateClosed {
		t.Errorf("Expected initial state to be CLOSED, got %v", cb.State())
	}

	if cb.Failures() != 0 {
		t.Errorf("Expected initial failures to be 0, got %d", cb.Failures())
	}
}

func TestCircuitBreaker_SuccessfulExecution(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	called := false
	err := cb.Execute(context.Background(), func() error {
		called = true
		return nil
	})

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if !called {
		t.Error("Expected function to be called")
	}

	if cb.State() != StateClosed {
		t.Errorf("Expected state to remain CLOSED, got %v", cb.State())
	}

	if cb.Failures() != 0 {
		t.Errorf("Expected failures to remain 0, got %d", cb.Failures())
	}
}

func TestCircuitBreaker_FailuresLeadToOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		MaxFailures:           3,
		Timeout:               100 * time.Millisecond,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      2,
		RequestTimeout:        10 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	// Execute failures up to the threshold
	for i := 0; i < config.MaxFailures; i++ {
		err := cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})

		if err == nil {
			t.Errorf("Expected error for failure %d", i)
		}

		if i < config.MaxFailures-1 && cb.State() != StateClosed {
			t.Errorf("Expected state to be CLOSED after %d failures, got %v", i+1, cb.State())
		}
	}

	// Circuit should now be open
	if cb.State() != StateOpen {
		t.Errorf("Expected state to be OPEN after %d failures, got %v", config.MaxFailures, cb.State())
	}

	// Next call should fail immediately
	err := cb.Execute(context.Background(), func() error {
		t.Error("Function should not be called when circuit is open")
		return nil
	})

	if err != ErrCircuitBreakerOpen {
		t.Errorf("Expected ErrCircuitBreakerOpen, got %v", err)
	}
}

func TestCircuitBreaker_OpenToHalfOpenTransition(t *testing.T) {
	config := CircuitBreakerConfig{
		MaxFailures:           2,
		Timeout:               50 * time.Millisecond,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      2,
		RequestTimeout:        10 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	// Trigger circuit to open
	for i := 0; i < config.MaxFailures; i++ {
		cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected state to be OPEN, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(config.Timeout + 10*time.Millisecond)

	// Next call should transition to half-open
	called := false
	err := cb.Execute(context.Background(), func() error {
		called = true
		return nil
	})

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if !called {
		t.Error("Expected function to be called")
	}

	if cb.State() != StateHalfOpen {
		t.Errorf("Expected state to be HALF_OPEN, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	config := CircuitBreakerConfig{
		MaxFailures:           2,
		Timeout:               10 * time.Millisecond,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      2,
		RequestTimeout:        10 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	// Trigger circuit to open
	for i := 0; i < config.MaxFailures; i++ {
		cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
	}

	// Wait for timeout and transition to half-open
	time.Sleep(config.Timeout + 5*time.Millisecond)

	// Execute successful calls to reach success threshold
	for i := 0; i < config.SuccessThreshold; i++ {
		err := cb.Execute(context.Background(), func() error {
			return nil
		})

		if err != nil {
			t.Errorf("Expected no error for success %d, got %v", i, err)
		}
	}

	// Circuit should now be closed
	if cb.State() != StateClosed {
		t.Errorf("Expected state to be CLOSED after %d successes, got %v", config.SuccessThreshold, cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		MaxFailures:           2,
		Timeout:               10 * time.Millisecond,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      2,
		RequestTimeout:        10 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	// Trigger circuit to open
	for i := 0; i < config.MaxFailures; i++ {
		cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
	}

	// Wait for timeout and transition to half-open
	time.Sleep(config.Timeout + 5*time.Millisecond)

	// One success
	cb.Execute(context.Background(), func() error {
		return nil
	})

	if cb.State() != StateHalfOpen {
		t.Errorf("Expected state to be HALF_OPEN, got %v", cb.State())
	}

	// One failure should trigger open again
	err := cb.Execute(context.Background(), func() error {
		return errors.New("test error")
	})

	if err == nil {
		t.Error("Expected error")
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected state to be OPEN after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_RequestTimeout(t *testing.T) {
	config := CircuitBreakerConfig{
		MaxFailures:           3,
		Timeout:               100 * time.Millisecond,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      2,
		RequestTimeout:        20 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	start := time.Now()
	err := cb.Execute(context.Background(), func() error {
		time.Sleep(50 * time.Millisecond) // Longer than timeout
		return nil
	})
	duration := time.Since(start)

	if err != ErrCircuitBreakerTimeout {
		t.Errorf("Expected ErrCircuitBreakerTimeout, got %v", err)
	}

	// Should not wait the full 50ms
	if duration > 30*time.Millisecond {
		t.Errorf("Expected timeout around 20ms, got %v", duration)
	}

	// Failure should be recorded
	if cb.Failures() != 1 {
		t.Errorf("Expected 1 failure, got %d", cb.Failures())
	}
}

func TestCircuitBreaker_MaxConcurrentRequests(t *testing.T) {
	config := CircuitBreakerConfig{
		MaxFailures:           2,
		Timeout:               10 * time.Millisecond,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      5, // Need more successes to transition to closed
		RequestTimeout:        100 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	// Trigger circuit to open
	for i := 0; i < config.MaxFailures; i++ {
		cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected state to be OPEN, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(config.Timeout + 5*time.Millisecond)

	// Manually transition to half-open to avoid race condition
	cb.TransitionToHalfOpen()

	if cb.State() != StateHalfOpen {
		t.Errorf("Expected state to be HALF_OPEN, got %v", cb.State())
	}

	// Manually increment requests counter to simulate active request
	cb.beforeRequest() // This should increment requests to 1

	stats := cb.Stats()
	t.Logf("After beforeRequest: State=%v, Requests=%d", stats.State, stats.Requests)

	// Now try a second request - should be rejected
	err := cb.Execute(context.Background(), func() error {
		t.Error("Second concurrent request should not execute")
		return nil
	})

	if err != ErrCircuitBreakerOpen {
		t.Errorf("Expected ErrCircuitBreakerOpen for concurrent request, got %v", err)
	}

	// Clean up by calling afterRequest to decrement counter
	cb.afterRequest()
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitBreakerConfig{
		MaxFailures:           2,
		Timeout:               100 * time.Millisecond,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      2,
		RequestTimeout:        10 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	// Trigger circuit to open
	for i := 0; i < config.MaxFailures; i++ {
		cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected state to be OPEN, got %v", cb.State())
	}

	// Reset the circuit breaker
	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("Expected state to be CLOSED after reset, got %v", cb.State())
	}

	if cb.Failures() != 0 {
		t.Errorf("Expected failures to be 0 after reset, got %d", cb.Failures())
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	config := CircuitBreakerConfig{
		MaxFailures:           3,
		Timeout:               100 * time.Millisecond,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      2,
		RequestTimeout:        10 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	// Initial stats
	stats := cb.Stats()
	if stats.State != StateClosed {
		t.Errorf("Expected initial state CLOSED, got %v", stats.State)
	}
	if stats.Failures != 0 {
		t.Errorf("Expected initial failures 0, got %d", stats.Failures)
	}

	// Execute some failures
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
	}

	stats = cb.Stats()
	if stats.Failures != 2 {
		t.Errorf("Expected 2 failures, got %d", stats.Failures)
	}

	// One more failure to trigger open
	cb.Execute(context.Background(), func() error {
		return errors.New("test error")
	})

	stats = cb.Stats()
	if stats.State != StateOpen {
		t.Errorf("Expected state OPEN, got %v", stats.State)
	}
}

func TestRetryWithCircuitBreaker(t *testing.T) {
	retryConfig := RetryConfig{
		MaxRetries:        2,
		InitialBackoff:    1 * time.Millisecond,
		MaxBackoff:        10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Jitter:            false,
		RetryableErrors:   DefaultRetryableErrors,
	}

	cbConfig := CircuitBreakerConfig{
		MaxFailures:           3,
		Timeout:               50 * time.Millisecond,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      2,
		RequestTimeout:        10 * time.Millisecond,
	}

	cb := NewCircuitBreaker(cbConfig)

	attempts := 0
	err := RetryWithCircuitBreaker(context.Background(), retryConfig, cb, func() error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}

	if cb.State() != StateClosed {
		t.Errorf("Expected circuit breaker to remain CLOSED, got %v", cb.State())
	}
}

func TestRetryWithCircuitBreaker_CircuitOpen(t *testing.T) {
	retryConfig := RetryConfig{
		MaxRetries:        5,
		InitialBackoff:    1 * time.Millisecond,
		MaxBackoff:        10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Jitter:            false,
		RetryableErrors:   DefaultRetryableErrors,
	}

	cbConfig := CircuitBreakerConfig{
		MaxFailures:           2,
		Timeout:               100 * time.Millisecond,
		MaxConcurrentRequests: 1,
		SuccessThreshold:      2,
		RequestTimeout:        10 * time.Millisecond,
	}

	cb := NewCircuitBreaker(cbConfig)

	// Trigger circuit to open
	for i := 0; i < cbConfig.MaxFailures; i++ {
		cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected circuit breaker to be OPEN, got %v", cb.State())
	}

	// Retry should fail immediately without retries due to circuit breaker
	attempts := 0
	err := RetryWithCircuitBreaker(context.Background(), retryConfig, cb, func() error {
		attempts++
		return errors.New("test error")
	})

	if err == nil {
		t.Error("Expected error, got nil")
	}

	// The circuit breaker should prevent the function from being called at all
	// Since the circuit is open, RetryWithCircuitBreaker should fail immediately
	if attempts != 0 { // Should not attempt at all due to circuit breaker being open
		t.Errorf("Expected 0 attempts (circuit open), got %d", attempts)
	}
}
