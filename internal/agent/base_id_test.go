// Package agent provides tests for conversation ID generation.
package agent

import (
	"testing"
)

// TestGenerateConversationIDUniqueness tests that conversation IDs are unique
// even when generated in rapid succession.
func TestGenerateConversationIDUniqueness(t *testing.T) {
	t.Run("IDs_are_unique_in_rapid_succession", func(t *testing.T) {
		// Generate 100 IDs rapidly
		ids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			id := generateConversationID()
			if ids[id] {
				t.Errorf("Duplicate conversation ID generated: %s", id)
			}
			ids[id] = true
		}
	})

	t.Run("IDs_have_valid_format", func(t *testing.T) {
		id := generateConversationID()
		if len(id) == 0 {
			t.Error("Expected non-empty conversation ID")
		}
		if id[:5] != "conv-" {
			t.Errorf("Expected ID to start with 'conv-', got '%s'", id[:5])
		}
	})
}
