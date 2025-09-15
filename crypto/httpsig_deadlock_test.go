package crypto

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestCloseDeadlockPrevention(t *testing.T) {
	t.Run("Close unblocks writer even when goroutine is blocked", func(t *testing.T) {
		// Create pipe and reader
		pr, pw := io.Pipe()
		reader := &StreamingSignatureReader{
			pr:      pr,
			pw:      pw,
			timeout: 100 * time.Millisecond,
		}
		reader.wg.Add(1)

		// Start a goroutine that tries to write and then hangs
		go func() {
			defer reader.wg.Done()

			// Write data that might block if reader is closed
			data := make([]byte, 64*1024) // Large write that could block
			for i := range data {
				data[i] = byte(i % 256)
			}

			// This write might block if the reader side is closed
			pw.Write(data)

			// Simulate some processing that might hang
			time.Sleep(50 * time.Millisecond)

			// Close the writer normally
			pw.Close()
		}()

		// Give the goroutine a moment to start writing
		time.Sleep(10 * time.Millisecond)

		start := time.Now()

		// Close should complete quickly even if goroutine is blocked on write
		err := reader.Close()
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Should complete quickly (well before timeout)
		if elapsed > 200*time.Millisecond {
			t.Fatalf("Close took too long: %v", elapsed)
		}

		t.Logf("✓ Close completed in %v without deadlock", elapsed)
	})

	t.Run("Close handles goroutine that never calls Done", func(t *testing.T) {
		// Test the timeout protection when goroutine truly hangs
		pr, pw := io.Pipe()
		reader := &StreamingSignatureReader{
			pr:      pr,
			pw:      pw,
			timeout: 50 * time.Millisecond, // Short timeout for test
		}
		reader.wg.Add(1)

		// Start a goroutine that never calls Done()
		go func() {
			// Write some data
			pw.Write([]byte("test data"))
			pw.Close()
			// Deliberately do NOT call reader.wg.Done() - simulate hang
			time.Sleep(1 * time.Hour)
		}()

		start := time.Now()

		// Close should timeout gracefully
		err := reader.Close()
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("Expected timeout error from Close")
		}

		// Should be our predefined timeout error
		if !errors.Is(err, ErrSignatureComputationTimeout) {
			t.Fatalf("Expected ErrSignatureComputationTimeout, got: %v", err)
		}

		// Should timeout in approximately 50ms
		if elapsed < 40*time.Millisecond || elapsed > 100*time.Millisecond {
			t.Logf("Warning: timeout took %v, expected ~50ms", elapsed)
		}

		t.Logf("✓ Close timed out gracefully in %v", elapsed)
	})

	t.Run("Close order prevents writer blocking", func(t *testing.T) {
		// Verify that closing pipe reader first unblocks pipe writer
		pr, pw := io.Pipe()
		reader := &StreamingSignatureReader{
			pr:      pr,
			pw:      pw,
			timeout: 1 * time.Second,
		}
		reader.wg.Add(1)

		writerBlocked := make(chan bool, 1)
		writerUnblocked := make(chan bool, 1)

		// Start goroutine that writes large data (will block when pipe buffer fills)
		go func() {
			defer reader.wg.Done()

			// Write large amount of data to fill pipe buffer
			largeData := make([]byte, 1024*1024) // 1MB

			// This write will block when pipe buffer is full
			writerBlocked <- true
			_, err := pw.Write(largeData)
			writerUnblocked <- true

			if err != nil {
				// Expected - pipe was closed
			}

			pw.Close()
		}()

		// Wait for writer to start blocking
		<-writerBlocked

		// Give writer time to fill buffer and block
		time.Sleep(10 * time.Millisecond)

		start := time.Now()

		// Close should unblock the writer quickly
		err := reader.Close()
		elapsed := time.Since(start)

		// Wait for writer to be unblocked
		select {
		case <-writerUnblocked:
			t.Log("✓ Writer was unblocked by Close()")
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Writer was not unblocked by Close()")
		}

		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Should complete quickly since we close pipe reader first
		if elapsed > 100*time.Millisecond {
			t.Fatalf("Close took too long: %v", elapsed)
		}

		t.Logf("✓ Close completed in %v and unblocked writer", elapsed)
	})
}
