// Package sandbox provides secure execution environment for operations.
package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSandboxConfig tests sandbox configuration.
func TestSandboxConfig(t *testing.T) {
	t.Run("default_config", func(t *testing.T) {
		config := DefaultConfig()

		if config.Timeout != 30*time.Second {
			t.Errorf("Expected default timeout 30s, got %v", config.Timeout)
		}
		if config.AllowNetwork {
			t.Error("Expected network to be disabled by default")
		}
		if config.MaxMemoryMB != 512 {
			t.Errorf("Expected default memory limit 512MB, got %d", config.MaxMemoryMB)
		}
	})

	t.Run("custom_config", func(t *testing.T) {
		config := &SandboxConfig{
			Timeout:      10 * time.Second,
			AllowNetwork: true,
			AllowWrite:   []string{"/tmp", "/var/tmp"},
			MaxMemoryMB:  1024,
		}

		if config.Timeout != 10*time.Second {
			t.Errorf("Expected timeout 10s, got %v", config.Timeout)
		}
		if !config.AllowNetwork {
			t.Error("Expected network to be enabled")
		}
	})
}

// TestNewExecutor tests executor creation.
func TestNewExecutor(t *testing.T) {
	t.Run("creates_executor", func(t *testing.T) {
		exec := NewExecutor(DefaultConfig())
		if exec == nil {
			t.Fatal("Expected non-nil executor")
		}
	})

	t.Run("executor_with_custom_config", func(t *testing.T) {
		config := &SandboxConfig{
			Timeout:     5 * time.Second,
			AllowWrite:  []string{"/tmp"},
			MaxMemoryMB: 256,
		}

		exec := NewExecutor(config)
		if exec == nil {
			t.Fatal("Expected non-nil executor")
		}
	})
}

// TestExecuteCommand tests command execution.
func TestExecuteCommand(t *testing.T) {
	exec := NewExecutor(DefaultConfig())

	t.Run("executes_simple_command", func(t *testing.T) {
		// Use echo command which is safe and portable
		result, err := exec.Execute(context.Background(), []string{"echo", "hello"})

		if err != nil {
			t.Fatalf("Failed to execute command: %v", err)
		}

		if result.ExitCode != 0 {
			t.Errorf("Expected exit code 0, got %d", result.ExitCode)
		}

		if !strings.Contains(result.Output, "hello") {
			t.Errorf("Expected output to contain 'hello', got '%s'", result.Output)
		}
	})

	t.Run("times_out_long_command", func(t *testing.T) {
		config := &SandboxConfig{
			Timeout: 100 * time.Millisecond,
		}
		exec := NewExecutor(config)

		// Sleep command that will timeout
		result, err := exec.Execute(context.Background(), []string{"sleep", "10"})

		if err == nil {
			t.Error("Expected timeout error")
		}

		if !result.TimedOut {
			t.Error("Expected timed out to be true")
		}
	})

	t.Run("handles_command_error", func(t *testing.T) {
		// Command that will fail
		result, err := exec.Execute(context.Background(), []string{"ls", "/nonexistent"})

		if err != nil {
			t.Logf("Command failed as expected: %v", err)
		}

		if result.ExitCode == 0 {
			t.Error("Expected non-zero exit code for failed command")
		}
	})

	t.Run("context_cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		result, err := exec.Execute(ctx, []string{"echo", "test"})

		if err == nil {
			t.Error("Expected error with cancelled context")
		}

		t.Logf("Context cancellation result: %v, err: %v", result, err)
	})
}

// TestPathValidation tests path validation.
func TestPathValidation(t *testing.T) {
	t.Run("allows_whitelisted_paths", func(t *testing.T) {
		config := &SandboxConfig{
			AllowWrite: []string{"/tmp", "/var/tmp"},
		}
		exec := NewExecutor(config)

		tests := []struct {
			path    string
			allowed bool
		}{
			{"/tmp/file.txt", true},
			{"/var/tmp/file.txt", true},
			{"/etc/passwd", false},
			{"/home/user/file.txt", false},
		}

		for _, tt := range tests {
			t.Run(tt.path, func(t *testing.T) {
				allowed := exec.IsPathAllowed(tt.path)
				if allowed != tt.allowed {
					t.Errorf("Expected allowed=%v for path %s, got %v", tt.allowed, tt.path, allowed)
				}
			})
		}
	})

	t.Run("empty_whitelist_blocks_all_writes", func(t *testing.T) {
		config := &SandboxConfig{
			AllowWrite: []string{},
		}
		exec := NewExecutor(config)

		if exec.IsPathAllowed("/tmp/file.txt") {
			t.Error("Expected all paths to be blocked with empty whitelist")
		}
	})
}

// TestNetworkControl tests network access control.
func TestNetworkControl(t *testing.T) {
	t.Run("blocks_network_by_default", func(t *testing.T) {
		config := DefaultConfig()
		if config.AllowNetwork {
			t.Error("Expected network to be blocked by default")
		}
	})

	t.Run("allows_network_when_configured", func(t *testing.T) {
		config := &SandboxConfig{
			AllowNetwork: true,
		}
		if !config.AllowNetwork {
			t.Error("Expected network to be allowed")
		}
	})
}

// TestResourceLimits tests resource limit enforcement.
func TestResourceLimits(t *testing.T) {
	t.Run("enforces_memory_limit", func(t *testing.T) {
		config := &SandboxConfig{
			MaxMemoryMB: 100,
		}
		exec := NewExecutor(config)

		if exec.config.MaxMemoryMB != 100 {
			t.Errorf("Expected memory limit 100MB, got %d", exec.config.MaxMemoryMB)
		}
	})

	t.Run("validates_timeout_range", func(t *testing.T) {
		tests := []struct {
			timeout time.Duration
			valid   bool
		}{
			{1 * time.Second, true},
			{30 * time.Second, true},
			{60 * time.Second, true},
			{0, false}, // Zero timeout invalid
			{-1 * time.Second, false}, // Negative timeout invalid
		}

		for _, tt := range tests {
			t.Run(tt.timeout.String(), func(t *testing.T) {
				config := &SandboxConfig{Timeout: tt.timeout}
				valid := config.Timeout > 0

				if valid != tt.valid {
					t.Errorf("Expected valid=%v for timeout %v", tt.valid, tt.timeout)
				}
			})
		}
	})
}

// TestSandboxIsolation tests isolation mechanisms.
func TestSandboxIsolation(t *testing.T) {
	t.Run("creates_isolated_environment", func(t *testing.T) {
		config := &SandboxConfig{
			AllowWrite: []string{"/tmp"},
		}
		exec := NewExecutor(config)

		// Test that isolated environment can be created
		if exec == nil {
			t.Fatal("Expected executor to be created")
		}
	})

	t.Run("prevents_path_traversal", func(t *testing.T) {
		config := &SandboxConfig{
			AllowWrite: []string{"/tmp"},
		}
		exec := NewExecutor(config)

		// Try various path traversal attempts
		traversalPaths := []string{
			"/tmp/../../../etc/passwd",
			"/tmp/../etc/passwd",
			"/../../../../etc/passwd",
		}

		for _, path := range traversalPaths {
			allowed := exec.IsPathAllowed(path)
			if allowed {
				t.Errorf("Path traversal should be blocked: %s", path)
			}
		}
	})
}

// TestResultHandling tests result handling.
func TestResultHandling(t *testing.T) {
	exec := NewExecutor(DefaultConfig())

	t.Run("returns_output", func(t *testing.T) {
		result, err := exec.Execute(context.Background(), []string{"echo", "-n", "test"})

		if err != nil {
			t.Fatalf("Failed to execute: %v", err)
		}

		if result.Output != "test" {
			t.Errorf("Expected output 'test', got '%s'", result.Output)
		}
	})

	t.Run("returns_error_output", func(t *testing.T) {
		result, err := exec.Execute(context.Background(), []string{"ls", "/nonexistent"})

		// Should have error output
		if result.Error == "" && result.ExitCode == 0 {
			t.Error("Expected error output or non-zero exit code")
		}

		t.Logf("Error output: %s", result.Error)
		_ = err // Command may fail, which is expected
	})
}

// TestEnvironmentVariables tests environment variable handling.
func TestEnvironmentVariables(t *testing.T) {
	t.Run("can_set_environment_variables", func(t *testing.T) {
		config := DefaultConfig()
		exec := NewExecutor(config)

		// Set environment variable
		exec.SetEnv("TEST_VAR", "test_value")

		result, err := exec.Execute(context.Background(), []string{"sh", "-c", "echo $TEST_VAR"})

		if err != nil {
			t.Fatalf("Failed to execute: %v", err)
		}

		if !strings.Contains(result.Output, "test_value") {
			t.Errorf("Expected environment variable to be set, got output: %s", result.Output)
		}
	})
}

// TestWorkingDirectory tests working directory handling.
func TestWorkingDirectory(t *testing.T) {
	t.Run("can_set_working_directory", func(t *testing.T) {
		config := DefaultConfig()
		exec := NewExecutor(config)

		// Set working directory to /tmp
		exec.SetWorkingDir("/tmp")

		result, err := exec.Execute(context.Background(), []string{"pwd"})

		if err != nil {
			t.Fatalf("Failed to execute: %v", err)
		}

		if !strings.Contains(result.Output, "/tmp") {
			t.Errorf("Expected working directory /tmp, got: %s", result.Output)
		}
	})
}

// TestTimeoutVariations tests different timeout scenarios.
func TestTimeoutVariations(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		cmd      []string
		expectedTimeout bool
	}{
		{
			name:           "quick_command_no_timeout",
			timeout:        1 * time.Second,
			cmd:            []string{"echo", "quick"},
			expectedTimeout: false,
		},
		{
			name:           "sleep_command_timeout",
			timeout:        100 * time.Millisecond,
			cmd:            []string{"sleep", "5"},
			expectedTimeout: true,
		},
		{
			name:           "moderate_timeout",
			timeout:        500 * time.Millisecond,
			cmd:            []string{"sleep", "0.1"},
			expectedTimeout: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &SandboxConfig{Timeout: tt.timeout}
			exec := NewExecutor(config)

			result, err := exec.Execute(context.Background(), tt.cmd)

			if tt.expectedTimeout && !result.TimedOut {
				t.Errorf("Expected timeout for command %v", tt.cmd)
			}
			if !tt.expectedTimeout && result.TimedOut {
				t.Errorf("Did not expect timeout for command %v", tt.cmd)
			}

			t.Logf("Result: timeout=%v, exitCode=%d, err=%v", result.TimedOut, result.ExitCode, err)
		})
	}
}
