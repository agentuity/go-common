package gravity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"time"
)

// RetryConfig defines configuration for retry logic
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts
	MaxRetries int

	// InitialBackoff is the initial backoff duration
	InitialBackoff time.Duration

	// MaxBackoff is the maximum backoff duration
	MaxBackoff time.Duration

	// BackoffMultiplier is the multiplier for exponential backoff
	BackoffMultiplier float64

	// Jitter adds randomness to backoff to avoid thundering herd
	Jitter bool

	// RetryableErrors is a function that determines if an error is retryable
	RetryableErrors func(error) bool
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        10 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            true,
		RetryableErrors:   DefaultRetryableErrors,
	}
}

// DefaultRetryableErrors determines if an error is retryable by default
func DefaultRetryableErrors(err error) bool {
	if err == nil {
		return false
	}

	// Don't retry circuit breaker errors
	if err == ErrCircuitBreakerOpen || err == ErrCircuitBreakerTimeout {
		return false
	}

	// Don't retry context cancellation
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	if errors.Is(err, io.EOF) {
		return false
	}

	// Retry network and temporary errors by default
	return true
}

// RetryableFunc is a function that can be retried
type RetryableFunc func() error

// Retry executes a function with retry logic
func Retry(ctx context.Context, config RetryConfig, fn RetryableFunc) error {
	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// Execute the function
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if config.RetryableErrors != nil && !config.RetryableErrors(err) {
			return fmt.Errorf("non-retryable error: %w", err)
		}

		// Don't sleep after the last attempt
		if attempt == config.MaxRetries {
			break
		}

		// Calculate backoff duration
		backoff := calculateBackoff(attempt, config)

		// Wait for backoff or context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(backoff):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("max retries exceeded (%d): %w", config.MaxRetries, lastErr)
}

// calculateBackoff calculates the backoff duration for a given attempt
func calculateBackoff(attempt int, config RetryConfig) time.Duration {
	// Calculate exponential backoff
	backoff := float64(config.InitialBackoff) * math.Pow(config.BackoffMultiplier, float64(attempt))

	// Apply maximum backoff limit
	if backoff > float64(config.MaxBackoff) {
		backoff = float64(config.MaxBackoff)
	}

	// Add jitter to avoid thundering herd
	if config.Jitter {
		jitter := rand.Float64() * 0.1 * backoff // 10% jitter
		backoff += jitter
	}

	return time.Duration(backoff)
}

// RetryWithCircuitBreaker combines retry logic with circuit breaker
func RetryWithCircuitBreaker(ctx context.Context, retryConfig RetryConfig, circuitBreaker *CircuitBreaker, fn RetryableFunc) error {
	retryFn := func() error {
		return circuitBreaker.Execute(ctx, fn)
	}

	return Retry(ctx, retryConfig, retryFn)
}

// ExponentialBackoff is a convenience function for exponential backoff retry
func ExponentialBackoff(ctx context.Context, maxRetries int, initialBackoff time.Duration, fn RetryableFunc) error {
	config := RetryConfig{
		MaxRetries:        maxRetries,
		InitialBackoff:    initialBackoff,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            true,
		RetryableErrors:   DefaultRetryableErrors,
	}

	return Retry(ctx, config, fn)
}

// LinearBackoff is a convenience function for linear backoff retry
func LinearBackoff(ctx context.Context, maxRetries int, backoffDuration time.Duration, fn RetryableFunc) error {
	config := RetryConfig{
		MaxRetries:        maxRetries,
		InitialBackoff:    backoffDuration,
		MaxBackoff:        backoffDuration,
		BackoffMultiplier: 1.0, // No exponential growth
		Jitter:            false,
		RetryableErrors:   DefaultRetryableErrors,
	}

	return Retry(ctx, config, fn)
}

// RetryStats tracks retry statistics
type RetryStats struct {
	TotalAttempts   int
	SuccessfulCalls int
	FailedCalls     int
	TotalRetries    int
	AverageBackoff  time.Duration
	LastError       error
}

// RetryWithStats executes a function with retry logic and tracks statistics
func RetryWithStats(ctx context.Context, config RetryConfig, fn RetryableFunc) (RetryStats, error) {
	stats := RetryStats{}
	var totalBackoff time.Duration
	var backoffCount int

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		stats.TotalAttempts++

		// Execute the function
		err := fn()
		if err == nil {
			stats.SuccessfulCalls++
			if backoffCount > 0 {
				stats.AverageBackoff = totalBackoff / time.Duration(backoffCount)
			}
			return stats, nil
		}

		stats.LastError = err

		// Check if error is retryable
		if config.RetryableErrors != nil && !config.RetryableErrors(err) {
			stats.FailedCalls++
			return stats, fmt.Errorf("non-retryable error: %w", err)
		}

		// Don't sleep after the last attempt
		if attempt == config.MaxRetries {
			break
		}

		stats.TotalRetries++

		// Calculate backoff duration
		backoff := calculateBackoff(attempt, config)
		totalBackoff += backoff
		backoffCount++

		// Wait for backoff or context cancellation
		select {
		case <-ctx.Done():
			stats.FailedCalls++
			return stats, fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(backoff):
			// Continue to next attempt
		}
	}

	stats.FailedCalls++
	if backoffCount > 0 {
		stats.AverageBackoff = totalBackoff / time.Duration(backoffCount)
	}

	return stats, fmt.Errorf("max retries exceeded (%d): %w", config.MaxRetries, stats.LastError)
}
