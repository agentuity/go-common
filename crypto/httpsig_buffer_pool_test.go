package crypto

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestBufferPoolConcurrentCorrectness(t *testing.T) {
	// Test that the buffer pool works correctly under concurrent access

	testData := strings.Repeat("Buffer pool concurrent test data. ", 1000) // ~34KB of data

	// Run multiple concurrent copy operations
	const numOperations = 50
	var wg sync.WaitGroup

	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			src := strings.NewReader(testData)
			var dst strings.Builder

			err := contextAwareCopy(ctx, &dst, src)
			if err != nil {
				t.Errorf("Unexpected error in contextAwareCopy: %v", err)
				return
			}

			if dst.String() != testData {
				t.Errorf("Data mismatch in concurrent operation")
			}
		}()
	}

	wg.Wait()

	t.Logf("✓ Successfully ran %d concurrent operations", numOperations)
	t.Log("✓ Buffer pool handles concurrent access correctly")
}

func TestBufferPoolCorrectness(t *testing.T) {
	// Verify that buffer reuse doesn't cause data corruption

	testCases := []string{
		"Short data",
		strings.Repeat("Medium length data. ", 100),
		strings.Repeat("Long data that spans multiple buffer fills. ", 500),
		"", // Empty data
		"Single character: X",
	}

	for i, testData := range testCases {
		t.Run(fmt.Sprintf("TestCase_%d", i), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			src := strings.NewReader(testData)
			var dst strings.Builder

			err := contextAwareCopy(ctx, &dst, src)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if dst.String() != testData {
				t.Fatalf("Data corruption detected: expected %q, got %q", testData, dst.String())
			}
		})
	}

	t.Log("✓ Buffer pool maintains data integrity")
}

func TestBufferPoolConcurrentStress(t *testing.T) {
	// Stress test concurrent access to the buffer pool

	const (
		numGoroutines          = 100
		operationsPerGoroutine = 50
	)

	testData := "Concurrent stress test data for buffer pool validation."

	var wg sync.WaitGroup
	var errorCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				ctx, cancel := context.WithCancel(context.Background())

				src := strings.NewReader(testData)
				var dst strings.Builder

				err := contextAwareCopy(ctx, &dst, src)
				cancel() // Cancel context after operation

				if err != nil {
					t.Errorf("Worker %d, operation %d failed: %v", workerID, j, err)
					atomic.AddInt32(&errorCount, 1)
					continue
				}

				if dst.String() != testData {
					t.Errorf("Worker %d, operation %d: data mismatch", workerID, j)
					atomic.AddInt32(&errorCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt32(&errorCount) > 0 {
		t.Fatalf("Concurrent stress test failed with %d errors", errorCount)
	}

	totalOperations := numGoroutines * operationsPerGoroutine
	t.Logf("✓ Successfully completed %d concurrent operations across %d goroutines", totalOperations, numGoroutines)
}

func BenchmarkBufferPoolAllocation(b *testing.B) {
	// Benchmark buffer pool allocation behavior
	testData := strings.Repeat("Buffer pool allocation benchmark. ", 100)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	// Measure allocations per operation
	allocsPerOp := testing.AllocsPerRun(100, func() {
		src := strings.NewReader(testData)
		var dst strings.Builder

		err := contextAwareCopy(ctx, &dst, src)
		if err != nil {
			b.Fatal(err)
		}
	})

	b.Logf("Allocations per contextAwareCopy operation: %.2f", allocsPerOp)

	// Should have minimal allocations due to buffer pooling
	if allocsPerOp > 5 {
		b.Logf("Warning: High allocation count (%.2f), buffer pool may not be effective", allocsPerOp)
	}
}

func BenchmarkContextAwareCopyWithPool(b *testing.B) {
	testData := strings.Repeat("Benchmark data for buffer pool performance testing. ", 100)
	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		src := strings.NewReader(testData)
		var dst strings.Builder

		err := contextAwareCopy(ctx, &dst, src)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkContextAwareCopyWithoutPool(b *testing.B) {
	// Benchmark the old approach (allocating buffers each time)
	testData := strings.Repeat("Benchmark data for non-pooled performance testing. ", 100)
	ctx := context.Background()

	contextAwareCopyWithoutPool := func(ctx context.Context, dst io.Writer, src io.Reader) error {
		// Old approach - allocate buffer each time
		buf := make([]byte, 8*1024)

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			n, readErr := src.Read(buf)

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if n > 0 {
				_, writeErr := dst.Write(buf[:n])
				if writeErr != nil {
					return writeErr
				}
			}

			if readErr != nil {
				if readErr == io.EOF {
					return nil
				}
				return readErr
			}
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		src := strings.NewReader(testData)
		var dst strings.Builder

		err := contextAwareCopyWithoutPool(ctx, &dst, src)
		if err != nil {
			b.Fatal(err)
		}
	}
}
