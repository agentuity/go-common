package gravity

import (
	"fmt"
	"testing"
)

func TestGetBufferedNonceChecker(t *testing.T) {
	t.Run("basic functionality", func(t *testing.T) {
		queueSize := 5
		checker := getBufferedNonceChecker(queueSize)

		// Test first nonce should pass
		err := checker("nonce1")
		if err != nil {
			t.Errorf("first nonce should pass, got error: %v", err)
		}

		// Test same nonce should fail
		err = checker("nonce1")
		if err == nil {
			t.Error("duplicate nonce should fail")
		}
		if err.Error() != "nonce already used" {
			t.Errorf("expected 'nonce already used' error, got: %v", err)
		}
	})

	t.Run("rolling buffer eviction", func(t *testing.T) {
		queueSize := 3
		checker := getBufferedNonceChecker(queueSize)

		// Fill the buffer
		for i := 0; i < queueSize; i++ {
			err := checker(fmt.Sprintf("nonce%d", i))
			if err != nil {
				t.Errorf("nonce%d should pass, got error: %v", i, err)
			}
		}

		// Verify all nonces are still tracked
		for i := 0; i < queueSize; i++ {
			err := checker(fmt.Sprintf("nonce%d", i))
			if err == nil {
				t.Errorf("nonce%d should be rejected as duplicate", i)
			}
		}

		// Add one more nonce to trigger eviction of the oldest
		err := checker("nonce3")
		if err != nil {
			t.Errorf("nonce3 should pass, got error: %v", err)
		}

		// The oldest nonce (nonce0) should now be reusable
		err = checker("nonce0")
		if err != nil {
			t.Errorf("nonce0 should be reusable after eviction, got error: %v", err)
		}
		// this also evicted nonce1

		// current nonces: nonce2, nonce3, nonce0

		// But nonce2 and nonce3 should still be tracked
		err = checker("nonce2")
		if err == nil {
			t.Error("nonce1 should still be tracked")
		}
		err = checker("nonce3")
		if err == nil {
			t.Error("nonce2 should still be tracked")
		}
	})

	t.Run("different nonces with same hash collision handling", func(t *testing.T) {
		checker := getBufferedNonceChecker(100)

		// Test with a variety of nonces
		nonces := []string{
			"test1", "test2", "test3",
			"different_string", "another_nonce",
			"", // empty string
			"very_long_nonce_string_to_test_hash_distribution_and_memory_usage",
		}

		// All should pass on first use
		for _, nonce := range nonces {
			err := checker(nonce)
			if err != nil {
				t.Errorf("nonce '%s' should pass, got error: %v", nonce, err)
			}
		}

		// All should fail on second use
		for _, nonce := range nonces {
			err := checker(nonce)
			if err == nil {
				t.Errorf("nonce '%s' should be rejected as duplicate", nonce)
			}
		}
	})

	t.Run("zero queue size", func(t *testing.T) {
		checker := getBufferedNonceChecker(0)

		// Should not panic and should allow reuse since buffer size is 0
		err := checker("test")
		if err != nil {
			t.Errorf("first use should pass, got error: %v", err)
		}

		// With queue size 0, the nonce should be immediately evicted
		// so it should be reusable
		err = checker("test")
		if err != nil {
			t.Errorf("with queue size 0, nonce should be reusable, got error: %v", err)
		}
	})

	t.Run("single item queue", func(t *testing.T) {
		checker := getBufferedNonceChecker(1)

		err := checker("nonce1")
		if err != nil {
			t.Errorf("nonce1 should pass, got error: %v", err)
		}

		// Should fail duplicate check
		err = checker("nonce1")
		if err == nil {
			t.Error("nonce1 should be rejected as duplicate")
		}

		// Adding second nonce should evict first
		err = checker("nonce2")
		if err != nil {
			t.Errorf("nonce2 should pass, got error: %v", err)
		}

		// nonce1 should now be reusable
		err = checker("nonce1")
		if err != nil {
			t.Errorf("nonce1 should be reusable after eviction, got error: %v", err)
		}
	})
}
