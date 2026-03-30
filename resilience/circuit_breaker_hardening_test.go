package resilience

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHardening_CircuitBreakerGoroutineChurn(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.RequestTimeout = 2 * time.Second
	cb := NewCircuitBreaker(config)

	baseline := runtime.NumGoroutine()

	var maxG atomic.Int64
	maxG.Store(int64(baseline))
	stop := make(chan struct{})
	doneSampler := make(chan struct{})
	go func() {
		defer close(doneSampler)
		ticker := time.NewTicker(100 * time.Microsecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				g := int64(runtime.NumGoroutine())
				for {
					prev := maxG.Load()
					if g <= prev || maxG.CompareAndSwap(prev, g) {
						break
					}
				}
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cb.Execute(context.Background(), func() error { return nil })
		}()
	}
	wg.Wait()
	close(stop)
	<-doneSampler

	spike := int(maxG.Load()) - baseline
	if spike < 50 {
		t.Fatalf("expected noticeable goroutine spike from Execute goroutine-per-call model, baseline=%d max=%d", baseline, maxG.Load())
	}
}

func TestHardening_CircuitBreakerConcurrentExecute(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.MaxFailures = 50
	config.RequestTimeout = 500 * time.Millisecond
	cb := NewCircuitBreaker(config)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cb.Execute(context.Background(), func() error {
				if i%3 == 0 {
					return errors.New("boom")
				}
				return nil
			})
		}()
	}
	wg.Wait()

	state := cb.State()
	if state != StateClosed && state != StateHalfOpen && state != StateOpen {
		t.Fatalf("invalid circuit breaker state: %v", state)
	}
}

func TestHardening_CircuitBreakerStateTransitionRace(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.MaxFailures = 2
	config.Timeout = 1 * time.Millisecond
	config.SuccessThreshold = 2
	config.RequestTimeout = 100 * time.Millisecond
	cb := NewCircuitBreaker(config)

	for i := 0; i < 200; i++ {
		if i%2 == 0 {
			_ = cb.Execute(context.Background(), func() error { return errors.New("fail") })
		} else {
			_ = cb.Execute(context.Background(), func() error { return nil })
		}
		time.Sleep(1 * time.Millisecond)
	}

	state := cb.State()
	if state != StateClosed && state != StateHalfOpen && state != StateOpen {
		t.Fatalf("breaker stuck in invalid state: %v", state)
	}
}

func TestHardening_CircuitBreakerHalfOpenConcurrency(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.MaxConcurrentRequests = 3
	config.RequestTimeout = 500 * time.Millisecond
	cb := NewCircuitBreaker(config)
	cb.TransitionToHalfOpen()

	start := make(chan struct{})
	release := make(chan struct{})
	results := make(chan error, config.MaxConcurrentRequests+1)

	for i := 0; i < config.MaxConcurrentRequests+1; i++ {
		go func() {
			<-start
			err := cb.Execute(context.Background(), func() error {
				<-release
				return nil
			})
			results <- err
		}()
	}

	close(start)
	time.Sleep(30 * time.Millisecond)
	close(release)

	rejected := 0
	for i := 0; i < config.MaxConcurrentRequests+1; i++ {
		err := <-results
		if errors.Is(err, ErrCircuitBreakerOpen) {
			rejected++
		}
	}

	if rejected < 1 {
		t.Fatalf("expected at least one request rejected in half-open (sent %d, limit %d), got %d rejections", config.MaxConcurrentRequests+1, config.MaxConcurrentRequests, rejected)
	}

	if got := atomic.LoadInt32(&cb.requests); got < 0 {
		t.Fatalf("requests counter went negative: %d", got)
	}
}

func TestHardening_CircuitBreakerTimeoutLeaksGoroutine(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.RequestTimeout = 10 * time.Millisecond
	cb := NewCircuitBreaker(config)

	baseline := runtime.NumGoroutine()
	release := make(chan struct{})
	var running atomic.Int64

	err := cb.Execute(context.Background(), func() error {
		running.Add(1)
		defer running.Add(-1)
		<-release
		return nil
	})

	if !errors.Is(err, ErrCircuitBreakerTimeout) {
		t.Fatalf("expected ErrCircuitBreakerTimeout, got %v", err)
	}

	// The worker goroutine is still blocked on <-release (Go cannot preempt
	// non-cooperative goroutines). Unblock it and verify it exits promptly —
	// proving the circuit breaker does not permanently leak goroutines.
	close(release)

	// Give the goroutine time to wind down.
	time.Sleep(50 * time.Millisecond)
	runtime.Gosched()

	if running.Load() != 0 {
		t.Fatalf("expected no in-flight goroutine after release; found %d still running", running.Load())
	}

	after := runtime.NumGoroutine()
	if after > baseline+2 {
		t.Fatalf("unexpected goroutine growth after timeout+release: baseline=%d after=%d", baseline, after)
	}
}

func TestHardening_CircuitBreakerExecuteContextCancellation(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.RequestTimeout = 5 * time.Second
	cb := NewCircuitBreaker(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	start := time.Now()
	err := cb.Execute(ctx, func() error {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		return nil
	})
	d := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
	if d > 100*time.Millisecond {
		t.Fatalf("expected fast cancellation path, took %v", d)
	}

	// Ensure the worker goroutine spawned by Execute completes before
	// the test exits, preventing interference with other tests.
	wg.Wait()
}
