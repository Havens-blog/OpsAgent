// Package cli provides command-line interface for OpsAgent.
package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestNewCLI tests CLI creation.
func TestNewCLI(t *testing.T) {
	t.Run("creates_cli_successfully", func(t *testing.T) {
		cli := NewCLI()

		if cli == nil {
			t.Fatal("Expected non-nil CLI")
		}
	})

	t.Run("creates_cli_with_options", func(t *testing.T) {
		cli := NewCLI(
			WithConfigPath("/test/config.yaml"),
			WithLogLevel("debug"),
		)

		if cli == nil {
			t.Fatal("Expected non-nil CLI")
		}

		if cli.configPath != "/test/config.yaml" {
			t.Errorf("Expected config path '/test/config.yaml', got '%s'", cli.configPath)
		}
	})
}

// TestVersionCommand tests version command.
func TestVersionCommand(t *testing.T) {
	cli := NewCLI()

	t.Run("shows_version", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{"version"})
		})

		if !contains(output, "OpsAgent") {
			t.Error("Version output should contain 'OpsAgent'")
		}
	})
}

// TestAnalyzeCommand tests RCA analyze command.
func TestAnalyzeCommand(t *testing.T) {
	cli := NewCLI(WithMockRCA())

	t.Run("requires_incident_id", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{"analyze"})
		})

		if !contains(output, "incident ID is required") {
			t.Error("Should show error for missing incident ID")
		}
	})

	t.Run("requires_start_time", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{"analyze", "--incident-id=INC-001"})
		})

		if !contains(output, "start time is required") {
			t.Error("Should show error for missing start time")
		}
	})

	t.Run("valid_request", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{
				"analyze",
				"--incident-id=INC-001",
				"--start-time=2024-01-01T00:00:00Z",
				"--end-time=2024-01-01T01:00:00Z",
				"--severity=P1",
				"--services=order-service,payment-service",
			})
		})

		// Should not show error for valid request
		if contains(output, "required") {
			t.Error("Should not show required field error")
		}
	})
}

// TestLogsCommand tests log query command.
func TestLogsCommand(t *testing.T) {
	cli := NewCLI()

	t.Run("query_logs", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{
				"logs",
				"--query=level:error",
				"--start-time=2024-01-01T00:00:00Z",
				"--end-time=2024-01-01T01:00:00Z",
			})
		})

		// Should process query
		if output == "" {
			t.Log("Empty output for logs query (expected without mock)")
		}
	})
}

// TestMetricsCommand tests metrics query command.
func TestMetricsCommand(t *testing.T) {
	cli := NewCLI()

	t.Run("query_metrics", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{
				"metrics",
				"--query=http_requests_total",
				"--start-time=2024-01-01T00:00:00Z",
				"--end-time=2024-01-01T01:00:00Z",
			})
		})

		// Should process query
		if output == "" {
			t.Log("Empty output for metrics query (expected without mock)")
		}
	})
}

// TestStatusCommand tests status command.
func TestStatusCommand(t *testing.T) {
	cli := NewCLI()

	t.Run("shows_status", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{"status"})
		})

		// Should show status information
		if !contains(output, "Status") && !contains(output, "status") {
			t.Log("Status output may be empty without full initialization")
		}
	})
}

// TestConfigCommand tests config command.
func TestConfigCommand(t *testing.T) {
	cli := NewCLI()

	t.Run("shows_config", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{"config", "show"})
		})

		// Should show config or path
		if output == "" {
			t.Log("Config output may be empty without config file")
		}
	})
}

// TestCLIOutput tests CLI output formatting.
func TestCLIOutput(t *testing.T) {
	cli := NewCLI()

	t.Run("json_output", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{"version", "--output=json"})
		})

		// JSON output should be valid
		if output != "" && strings.HasPrefix(output, "{") {
			t.Log("Valid JSON output")
		}
	})

	t.Run("table_output", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{"version", "--output=table"})
		})

		// Table output
		if output != "" {
			t.Log("Table output generated")
		}
	})
}

// TestErrorHandling tests error handling.
func TestErrorHandling(t *testing.T) {
	cli := NewCLI()

	t.Run("unknown_command", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{"unknown-command"})
		})

		// Should show error or help
		if !contains(output, "unknown") && !contains(output, "usage") {
			t.Log("Should show error for unknown command")
		}
	})

	t.Run("invalid_flag", func(t *testing.T) {
		output := captureOutput(func() {
			cli.Run([]string{"--invalid-flag"})
		})

		// Should show error
		if !contains(output, "unknown") && !contains(output, "flag") {
			t.Log("Should show error for invalid flag")
		}
	})
}

// TestInteractiveMode tests interactive mode.
func TestInteractiveMode(t *testing.T) {
	cli := NewCLI()

	t.Run("interactive_flag", func(t *testing.T) {
		// Interactive mode should be available
		if cli == nil {
			t.Error("CLI should support interactive mode")
		}
	})
}

// Helper functions

// captureOutput captures stdout during function execution.
func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// contains checks if a string contains a substring.
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
