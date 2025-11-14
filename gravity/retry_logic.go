package gravity

import (
	"github.com/agentuity/go-common/resilience"
)

// Re-export types and functions from resilience package for backward compatibility
type (
	RetryConfig          = resilience.RetryConfig
	RetryableFunc        = resilience.RetryableFunc
	RetryStats           = resilience.RetryStats
	CircuitBreaker       = resilience.CircuitBreaker
	CircuitBreakerConfig = resilience.CircuitBreakerConfig
	CircuitBreakerState  = resilience.CircuitBreakerState
	CircuitBreakerStats  = resilience.CircuitBreakerStats
)

const (
	StateClosed   = resilience.StateClosed
	StateHalfOpen = resilience.StateHalfOpen
	StateOpen     = resilience.StateOpen
)

var (
	ErrCircuitBreakerOpen    = resilience.ErrCircuitBreakerOpen
	ErrCircuitBreakerTimeout = resilience.ErrCircuitBreakerTimeout
	ErrTooManyFailures       = resilience.ErrTooManyFailures
)

// Re-export functions for backward compatibility
var (
	DefaultRetryConfig          = resilience.DefaultRetryConfig
	DefaultRetryableErrors      = resilience.DefaultRetryableErrors
	Retry                       = resilience.Retry
	RetryWithCircuitBreaker     = resilience.RetryWithCircuitBreaker
	ExponentialBackoff          = resilience.ExponentialBackoff
	LinearBackoff               = resilience.LinearBackoff
	RetryWithStats              = resilience.RetryWithStats
	NewCircuitBreaker           = resilience.NewCircuitBreaker
	DefaultCircuitBreakerConfig = resilience.DefaultCircuitBreakerConfig
)
