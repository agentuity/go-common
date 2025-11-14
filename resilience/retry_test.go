package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetry_Success(t *testing.T) {
	config := DefaultRetryConfig()
	attempts := 0

	err := Retry(context.Background(), config, func() error {
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
}

func TestRetry_MaxRetriesExceeded(t *testing.T) {
	config := RetryConfig{
		MaxRetries:        2,
		InitialBackoff:    1 * time.Millisecond,
		MaxBackoff:        10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Jitter:            false,
		RetryableErrors:   DefaultRetryableErrors,
	}

	attempts := 0
	err := Retry(context.Background(), config, func() error {
		attempts++
		return errors.New("persistent error")
	})

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if attempts != 3 { // Initial attempt + 2 retries
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestRetry_NonRetryableError(t *testing.T) {
	config := RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    1 * time.Millisecond,
		MaxBackoff:        10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Jitter:            false,
		RetryableErrors: func(err error) bool {
			return err.Error() != "non-retryable"
		},
	}

	attempts := 0
	err := Retry(context.Background(), config, func() error {
		attempts++
		return errors.New("non-retryable")
	})

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempts)
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	config := RetryConfig{
		MaxRetries:        5,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        1 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            false,
		RetryableErrors:   DefaultRetryableErrors,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	attempts := 0
	err := Retry(ctx, config, func() error {
		attempts++
		return errors.New("temporary error")
	})

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if attempts == 0 {
		t.Error("Expected at least 1 attempt")
	}

	// Should not retry much due to context timeout
	if attempts > 3 {
		t.Errorf("Expected few attempts due to timeout, got %d", attempts)
	}
}

func TestExponentialBackoff(t *testing.T) {
	attempts := 0
	start := time.Now()

	err := ExponentialBackoff(context.Background(), 2, 10*time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	duration := time.Since(start)

	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}

	// Should have some backoff delay
	minExpectedDuration := 10*time.Millisecond + 20*time.Millisecond // First + second backoff
	if duration < minExpectedDuration {
		t.Errorf("Expected at least %v delay, got %v", minExpectedDuration, duration)
	}
}

func TestRetryWithStats(t *testing.T) {
	config := RetryConfig{
		MaxRetries:        2,
		InitialBackoff:    5 * time.Millisecond,
		MaxBackoff:        50 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Jitter:            false,
		RetryableErrors:   DefaultRetryableErrors,
	}

	attempts := 0
	stats, err := RetryWithStats(context.Background(), config, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	if stats.TotalAttempts != 3 {
		t.Errorf("Expected 3 total attempts, got %d", stats.TotalAttempts)
	}

	if stats.SuccessfulCalls != 1 {
		t.Errorf("Expected 1 successful call, got %d", stats.SuccessfulCalls)
	}

	if stats.TotalRetries != 2 {
		t.Errorf("Expected 2 retries, got %d", stats.TotalRetries)
	}

	if stats.AverageBackoff <= 0 {
		t.Errorf("Expected positive average backoff, got %v", stats.AverageBackoff)
	}
}

func TestDefaultRetryableErrors(t *testing.T) {
	tests := []struct {
		err       error
		retryable bool
	}{
		{nil, false},
		{errors.New("network error"), true},
		{ErrCircuitBreakerOpen, false},
		{ErrCircuitBreakerTimeout, false},
		{context.Canceled, false},
		{context.DeadlineExceeded, false},
	}

	for _, tt := range tests {
		result := DefaultRetryableErrors(tt.err)
		if result != tt.retryable {
			t.Errorf("Error %v: expected retryable=%v, got %v", tt.err, tt.retryable, result)
		}
	}
}

func TestCalculateBackoff(t *testing.T) {
	config := RetryConfig{
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        1 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            false,
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, 1 * time.Second}, // Capped at MaxBackoff
		{5, 1 * time.Second}, // Still capped
	}

	for _, tt := range tests {
		result := calculateBackoff(tt.attempt, config)
		if result != tt.expected {
			t.Errorf("Attempt %d: expected %v, got %v", tt.attempt, tt.expected, result)
		}
	}
}

func TestCalculateBackoffWithJitter(t *testing.T) {
	config := RetryConfig{
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        1 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            true,
	}

	// Test that jitter produces different values
	results := make(map[time.Duration]bool)
	for range 10 {
		result := calculateBackoff(1, config)
		results[result] = true
	}

	// Should have multiple different values due to jitter
	if len(results) < 2 {
		t.Error("Expected jitter to produce different backoff values")
	}

	// All values should be around 200ms (base for attempt 1)
	for duration := range results {
		if duration < 180*time.Millisecond || duration > 240*time.Millisecond {
			t.Errorf("Jittered backoff %v outside expected range [180ms, 240ms]", duration)
		}
	}
}
