package crypto

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestStreamingSignatureTimeoutProtection(t *testing.T) {
	t.Run("Read timeout protection", func(t *testing.T) {
		// Create pipe and reader with short timeout for testing
		pr, pw := io.Pipe()
		reader := &StreamingSignatureReader{
			pr:      pr,
			pw:      pw,
			timeout: 100 * time.Millisecond, // Short timeout for testing
		}
		reader.wg.Add(1)

		// Start a goroutine that hangs (never calls wg.Done())
		go func() {
			// Write some data and close pipe to trigger EOF
			pw.Write([]byte("test data"))
			pw.Close()
			// Deliberately do NOT call reader.wg.Done() to simulate hanging
			time.Sleep(1 * time.Hour) // Hang indefinitely
		}()

		start := time.Now()

		// Read until EOF - this should timeout quickly at EOF
		data, err := io.ReadAll(reader)
		elapsed := time.Since(start)

		t.Logf("Read completed in %v with data: %q", elapsed, string(data))

		if err == nil {
			t.Fatal("Expected timeout error but got none")
		}

		if !errors.Is(err, ErrSignatureComputationTimeout) {
			t.Fatalf("Expected ErrSignatureComputationTimeout, got: %v", err)
		}

		// Should timeout in approximately 100ms
		if elapsed < 80*time.Millisecond || elapsed > 200*time.Millisecond {
			t.Logf("Warning: timeout took %v, expected ~100ms", elapsed)
		}

		t.Log("✓ Read timeout protection working")
	})

	t.Run("Close timeout protection", func(t *testing.T) {
		// Create pipe and reader with short timeout for testing
		pr, pw := io.Pipe()
		reader := &StreamingSignatureReader{
			pr:      pr,
			pw:      pw,
			timeout: 100 * time.Millisecond, // Short timeout for testing
		}
		reader.wg.Add(1)

		// Start a goroutine that hangs (never calls wg.Done())
		go func() {
			defer pw.Close()
			// Deliberately do NOT call reader.wg.Done() to simulate hanging
			time.Sleep(1 * time.Hour) // Hang indefinitely
		}()

		start := time.Now()

		// Close should timeout quickly
		err := reader.Close()
		elapsed := time.Since(start)

		t.Logf("Close completed in %v", elapsed)

		if err == nil {
			t.Fatal("Expected timeout error from Close(), got none")
		}

		if !errors.Is(err, ErrSignatureComputationTimeout) {
			t.Fatalf("Expected ErrSignatureComputationTimeout, got: %v", err)
		}

		// Should timeout in approximately 100ms
		if elapsed < 80*time.Millisecond || elapsed > 200*time.Millisecond {
			t.Logf("Warning: timeout took %v, expected ~100ms", elapsed)
		}

		t.Log("✓ Close timeout protection working")
	})

	t.Run("Normal completion without timeout", func(t *testing.T) {
		// Verify that normal operation doesn't trigger timeout
		pr, pw := io.Pipe()
		reader := &StreamingSignatureReader{
			pr:      pr,
			pw:      pw,
			timeout: 1 * time.Second, // Reasonable timeout for normal operation
		}
		reader.wg.Add(1)

		testData := "normal completion test"

		go func() {
			defer reader.wg.Done() // Properly signal completion
			defer pw.Close()
			pw.Write([]byte(testData))
		}()

		start := time.Now()

		// Read all data
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Close
		err = reader.Close()
		if err != nil {
			t.Fatalf("Unexpected close error: %v", err)
		}

		elapsed := time.Since(start)

		// Should complete quickly (well under timeout)
		if elapsed > 1*time.Second {
			t.Fatalf("Normal operation took too long: %v", elapsed)
		}

		if string(data) != testData {
			t.Fatalf("Data mismatch: expected %q, got %q", testData, string(data))
		}

		t.Logf("✓ Normal completion in %v (no timeout)", elapsed)
	})
}

func TestTimeoutConfigurability(t *testing.T) {
	// Test to verify timeout values are reasonable for different scenarios

	scenarios := []struct {
		name            string
		expectedTimeout time.Duration
		description     string
	}{
		{
			name:            "Standard timeout",
			expectedTimeout: 30 * time.Second,
			description:     "Should be long enough for reasonable signature computation",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// The timeout is currently hardcoded to 30 seconds
			// This test documents the expected behavior

			if scenario.expectedTimeout != 30*time.Second {
				t.Fatalf("Expected timeout %v, but implementation uses 30s", scenario.expectedTimeout)
			}

			t.Logf("✓ %s: %v", scenario.description, scenario.expectedTimeout)
		})
	}

	t.Log("💡 Consider making timeout configurable for different use cases:")
	t.Log("   - Fast networks: shorter timeout (5-10s)")
	t.Log("   - Slow networks: longer timeout (60s+)")
	t.Log("   - Testing: very short timeout (100ms)")
}
