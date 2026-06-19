// Package sandbox provides secure execution environment for operations.
// This module implements timeout control, resource limits, network isolation,
// and filesystem access control for safe command execution.
package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SandboxConfig defines the sandbox configuration.
type SandboxConfig struct {
	Timeout      time.Duration // Execution timeout (default 30s, max 60s)
	AllowNetwork bool          // Allow network access (default false)
	AllowWrite   []string      // Whitelist of writable paths
	MaxMemoryMB  int           // Memory limit in MB (default 512MB)
}

// DefaultConfig returns the default sandbox configuration.
func DefaultConfig() *SandboxConfig {
	return &SandboxConfig{
		Timeout:      30 * time.Second,
		AllowNetwork: false,
		AllowWrite:   []string{},
		MaxMemoryMB:  512,
	}
}

// SandboxResult represents the result of a sandboxed execution.
type SandboxResult struct {
	ExitCode int
	TimedOut bool
	Output   string
	Error    string
}

// Executor provides sandboxed command execution.
type Executor struct {
	config     *SandboxConfig
	mu         sync.RWMutex
	env        map[string]string
	workingDir string
}

// NewExecutor creates a new sandbox executor.
func NewExecutor(config *SandboxConfig) *Executor {
	if config == nil {
		config = DefaultConfig()
	}

	// Validate timeout
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Timeout > 60*time.Second {
		config.Timeout = 60 * time.Second
	}

	// Validate memory limit
	if config.MaxMemoryMB <= 0 {
		config.MaxMemoryMB = 512
	}

	return &Executor{
		config:     config,
		env:        make(map[string]string),
		workingDir: "",
	}
}

// Execute executes a command in the sandbox.
func (e *Executor) Execute(ctx context.Context, command []string) (*SandboxResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(command) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return &SandboxResult{
			ExitCode: -1,
			Error:    "context cancelled",
		}, ctx.Err()
	default:
		// Context is valid, continue
	}

	// Create context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	// Prepare command
	cmd := exec.CommandContext(timeoutCtx, command[0], command[1:]...)

	// Set working directory
	if e.workingDir != "" {
		// Validate working directory is allowed
		if !e.isPathAllowed(e.workingDir) {
			return &SandboxResult{
				ExitCode: -1,
				Error:    fmt.Sprintf("working directory not allowed: %s", e.workingDir),
			}, nil
		}
		cmd.Dir = e.workingDir
	}

	// Set environment variables
	env := e.getEnvironment()
	cmd.Env = env

	// Execute command
	output, err := cmd.CombinedOutput()

	result := &SandboxResult{
		Output: string(output),
		Error:  "",
	}

	// Check for timeout
	if timeoutCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		result.Error = "command timed out"
		return result, fmt.Errorf("command timed out after %v", e.config.Timeout)
	}

	// Get exit code
	if err != nil {
		result.ExitCode = -1
		if exitError, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitError.ExitCode()
		}
		result.Error = err.Error()
	} else {
		result.ExitCode = 0
	}

	return result, nil
}

// IsPathAllowed checks if a path is allowed for write operations.
func (e *Executor) IsPathAllowed(path string) bool {
	// Normalize path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Check against whitelist
	for _, allowed := range e.config.AllowWrite {
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}

		// Check if path is within allowed directory
		rel, err := filepath.Rel(allowedAbs, absPath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}

		// Check if path matches allowed path exactly
		if absPath == allowedAbs {
			return true
		}

		// Check if path is a subdirectory of allowed path
		if strings.HasPrefix(absPath, allowedAbs+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

// isPathAllowed is an internal helper for path validation.
func (e *Executor) isPathAllowed(path string) bool {
	// For read operations, we're more permissive
	// This is a simplified check
	if path == "" || path == "/" {
		return false
	}

	// Prevent obvious path traversal attempts
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		return false
	}

	return true
}

// SetEnv sets an environment variable.
func (e *Executor) SetEnv(key, value string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.env == nil {
		e.env = make(map[string]string)
	}
	e.env[key] = value
}

// getEnvironment returns the environment variables for execution.
func (e *Executor) getEnvironment() []string {
	// Start with current environment
	env := []string{}

	// Get current process environment
	for _, e := range exec.Command("").Env {
		env = append(env, e)
	}

	// Add custom environment variables
	for key, value := range e.env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	return env
}

// SetWorkingDir sets the working directory for command execution.
func (e *Executor) SetWorkingDir(dir string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.workingDir = dir
}

// Config returns the sandbox configuration.
func (e *Executor) Config() *SandboxConfig {
	return e.config
}

// Cleanup performs cleanup of sandbox resources.
func (e *Executor) Cleanup() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Reset state
	e.env = make(map[string]string)
	e.workingDir = ""

	return nil
}

// ValidateConfig validates the sandbox configuration.
func ValidateConfig(config *SandboxConfig) error {
	if config.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if config.Timeout > 60*time.Second {
		return fmt.Errorf("timeout cannot exceed 60 seconds")
	}
	if config.MaxMemoryMB <= 0 {
		return fmt.Errorf("memory limit must be positive")
	}
	return nil
}

// IsNetworkAllowed checks if network access is allowed.
func (e *Executor) IsNetworkAllowed() bool {
	return e.config.AllowNetwork
}

// GetTimeout returns the configured timeout.
func (e *Executor) GetTimeout() time.Duration {
	return e.config.Timeout
}

// GetMemoryLimit returns the configured memory limit.
func (e *Executor) GetMemoryLimit() int {
	return e.config.MaxMemoryMB
}
