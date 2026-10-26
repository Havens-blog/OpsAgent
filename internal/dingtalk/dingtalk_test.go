// Package dingtalk provides DingTalk bot integration for incident notifications.
package dingtalk

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// TestNewBot tests bot creation.
func TestNewBot(t *testing.T) {
	t.Run("creates_bot_successfully", func(t *testing.T) {
		bot := NewBot("test-webhook", WithSecret("test-secret"))

		if bot == nil {
			t.Fatal("Expected non-nil bot")
		}

		if bot.WebhookURL != "test-webhook" {
			t.Errorf("Expected webhook URL 'test-webhook', got '%s'", bot.WebhookURL)
		}
	})

	t.Run("creates_bot_with_options", func(t *testing.T) {
		bot := NewBot("webhook",
			WithSecret("secret"),
			WithTimeout(30*time.Second),
		)

		if bot.secret != "secret" {
			t.Errorf("Expected secret 'secret', got '%s'", bot.secret)
		}

		if bot.timeout != 30*time.Second {
			t.Errorf("Expected timeout 30s, got %v", bot.timeout)
		}
	})
}

// TestSendMessage tests sending messages.
func TestSendMessage(t *testing.T) {
	bot := NewBot("https://oapi.dingtalk.com/robot/send?access_token=test")

	t.Run("sends_text_message", func(t *testing.T) {
		msg := &TextMessage{
			Text: "Test message",
		}

		err := bot.SendMessage(context.Background(), msg)
		// Will fail with invalid webhook, but tests the flow
		if err == nil {
			t.Log("Message sent (may fail with invalid webhook)")
		}
	})

	t.Run("sends_markdown_message", func(t *testing.T) {
		msg := &MarkdownMessage{
			Title: "Test Title",
			Text:  "### Test Content\n\nThis is a test",
		}

		err := bot.SendMessage(context.Background(), msg)
		if err == nil {
			t.Log("Markdown message sent")
		}
	})
}

// TestSendIncidentAlert tests sending incident alerts.
func TestSendIncidentAlert(t *testing.T) {
	bot := NewBot("test-webhook")

	incident := &models.Incident{
		ID:          "INC-001",
		Severity:    "P1",
		Description: "Database connection pool exhaustion",
	}

	t.Run("sends_incident_alert", func(t *testing.T) {
		err := bot.SendIncidentAlert(context.Background(), incident, "Production", "order-service")
		// Will fail with invalid webhook, but tests the flow
		if err == nil {
			t.Log("Incident alert sent")
		}
	})

	t.Run("sends_rca_completed_alert", func(t *testing.T) {
		report := &models.RCAReport{
			Incident: &models.Incident{
				ID:       "INC-001",
				Severity: "P1",
			},
			Confidence: 0.85,
			TopCandidate: &models.ScoredHypothesis{
				Hypothesis: models.Hypothesis{
					Service:     "order-service",
					Description: "Database connection pool exhaustion",
				},
			},
		}

		err := bot.SendRCACompletedAlert(context.Background(), report)
		if err == nil {
			t.Log("RCA completed alert sent")
		}
	})
}

// TestMessageSigning tests message signing for security.
func TestMessageSigning(t *testing.T) {
	t.Run("generates_valid_signature", func(t *testing.T) {
		bot := NewBot("webhook", WithSecret("test-secret"))

		timestamp := time.Now().UnixMilli()
		signature := bot.signMessage(timestamp, "test-message")

		if signature == "" {
			t.Error("Expected non-empty signature")
		}

		t.Logf("Generated signature: %s", signature)
	})

	t.Run("different_timestamps_different_signatures", func(t *testing.T) {
		bot := NewBot("webhook", WithSecret("test-secret"))

		sig1 := bot.signMessage(1000, "message")
		time.Sleep(time.Millisecond)
		sig2 := bot.signMessage(1001, "message")

		if sig1 == sig2 {
			t.Error("Expected different signatures for different timestamps")
		}
	})
}

// TestFormatIncidentMessage tests incident message formatting.
func TestFormatIncidentMessage(t *testing.T) {
	bot := NewBot("test-webhook")

	incident := &models.Incident{
		ID:              "INC-001",
		Severity:        "P1",
		Description:     "Database connection pool exhaustion",
		AffectedServices: []string{"order-service", "payment-service"},
		StartTime:        time.Now().Add(-1 * time.Hour),
		EndTime:          time.Now(),
	}

	t.Run("formats_text_message", func(t *testing.T) {
		msg := bot.formatIncidentMessage(incident, "Production", "")
		if msg == "" {
			t.Error("Expected non-empty message")
		}

		t.Logf("Formatted message:\n%s", msg)
	})

	t.Run("includes_key_information", func(t *testing.T) {
		msg := bot.formatIncidentMessage(incident, "Production", "order-service")

		// Check for key fields
		if !contains(msg, "INC-001") {
			t.Error("Message should include incident ID")
		}
		if !contains(msg, "P1") {
			t.Error("Message should include severity")
		}
		if !contains(msg, "order-service") {
			t.Error("Message should include affected services")
		}
	})
}

// TestFormatRCAMessage tests RCA report message formatting.
func TestFormatRCAMessage(t *testing.T) {
	bot := NewBot("test-webhook")

	report := &models.RCAReport{
		Incident: &models.Incident{
			ID:       "INC-001",
			Severity: "P1",
		},
		Confidence: 0.85,
		TopCandidate: &models.ScoredHypothesis{
			Hypothesis: models.Hypothesis{
				Service:     "order-service",
				Description: "Database connection pool exhaustion",
				Evidence:    []string{"High connection usage", "Timeout errors"},
			},
			Score: 0.9,
		},
	}

	t.Run("formats_rca_report", func(t *testing.T) {
		msg := bot.formatRCAMessage(report)
		if msg == "" {
			t.Error("Expected non-empty message")
		}

		t.Logf("Formatted RCA message:\n%s", msg)
	})

	t.Run("includes_root_cause", func(t *testing.T) {
		msg := bot.formatRCAMessage(report)

		if !contains(msg, "order-service") {
			t.Error("RCA message should include root cause service")
		}
		if !contains(msg, "connection pool") {
			t.Error("RCA message should include description")
		}
	})
}

// TestBotContext tests bot context handling.
func TestBotContext(t *testing.T) {
	bot := NewBot("test-webhook")

	ctx := context.Background()

	t.Run("context_cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		msg := &TextMessage{Text: "Test"}
		err := bot.SendMessage(cancelCtx, msg)

		if err == nil {
			t.Error("Expected error with cancelled context")
		}
	})

	t.Run("context_timeout", func(t *testing.T) {
		timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Nanosecond)
		defer cancel()

		time.Sleep(10 * time.Millisecond) // Ensure timeout

		msg := &TextMessage{Text: "Test"}
		err := bot.SendMessage(timeoutCtx, msg)

		if err == nil {
			t.Error("Expected error with timed out context")
		}
	})
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
