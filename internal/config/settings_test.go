// Package config provides configuration management for OpsAgent.
package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	// Create a temporary directory for test configs
	tmpDir := t.TempDir()
	settingsFile := filepath.Join(tmpDir, "settings.yaml")

	// Write test settings
	testYAML := `
environment: development
server:
  host: "127.0.0.1"
  port: 9090
llm:
  provider: test-provider
  api_key: test-key-123
  model: test-model
`
	if err := os.WriteFile(settingsFile, []byte(testYAML), 0644); err != nil {
		t.Fatalf("failed to write test settings: %v", err)
	}

	// Load settings
	settings, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}

	// Verify loaded values
	if settings.Environment != EnvDevelopment {
		t.Errorf("expected environment %s, got %s", EnvDevelopment, settings.Environment)
	}
	if settings.Server.Host != "127.0.0.1" {
		t.Errorf("expected host 127.0.0.1, got %s", settings.Server.Host)
	}
	if settings.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", settings.Server.Port)
	}
	if settings.LLM.Provider != "test-provider" {
		t.Errorf("expected provider test-provider, got %s", settings.LLM.Provider)
	}
	if settings.LLM.APIKey != "test-key-123" {
		t.Errorf("expected api_key test-key-123, got %s", settings.LLM.APIKey)
	}
	if settings.LLM.Model != "test-model" {
		t.Errorf("expected model test-model, got %s", settings.LLM.Model)
	}
}

func TestExpandEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		setup    func()
	}{
		{
			name:     "no expansion needed",
			input:    "plain-text",
			expected: "plain-text",
			setup:    func() {},
		},
		{
			name:     "${VAR} syntax",
			input:    "${TEST_VAR}",
			expected: "test-value",
			setup: func() {
				os.Setenv("TEST_VAR", "test-value")
			},
		},
		{
			name:     "mixed content",
			input:    "prefix-${TEST_VAR}-suffix",
			expected: "prefix-test-value-suffix",
			setup: func() {
				os.Setenv("TEST_VAR", "test-value")
			},
		},
		{
			name:     "multiple variables",
			input:    "${VAR1}-${VAR2}",
			expected: "value1-value2",
			setup: func() {
				os.Setenv("VAR1", "value1")
				os.Setenv("VAR2", "value2")
			},
		},
		{
			name:     "unset variable",
			input:    "${UNSET_VAR}",
			expected: "",
			setup:    func() {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			defer func() {
				// Clean up env vars
				for _, v := range []string{"TEST_VAR", "VAR1", "VAR2"} {
					os.Unsetenv(v)
				}
			}()

			result := expandEnv(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnv(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLoadFromFileWithEnvExpansion(t *testing.T) {
	// Reset global settings for clean test
	globalSettings = nil
	once = sync.Once{}

	// Set up test environment variables
	os.Setenv("LLM_API_KEY", "env-api-key-456")
	os.Setenv("LLM_PROVIDER", "env-provider")
	defer func() {
		os.Unsetenv("LLM_API_KEY")
		os.Unsetenv("LLM_PROVIDER")
	}()

	// Create a temporary directory for test configs
	tmpDir := t.TempDir()
	settingsFile := filepath.Join(tmpDir, "settings.yaml")

	// Write test settings with environment variable placeholders
	testYAML := `
llm:
  provider: ${LLM_PROVIDER}
  api_key: ${LLM_API_KEY}
  model: qwen-plus
`
	if err := os.WriteFile(settingsFile, []byte(testYAML), 0644); err != nil {
		t.Fatalf("failed to write test settings: %v", err)
	}

	// Load settings
	settings, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}

	// Verify expanded values
	if settings.LLM.Provider != "env-provider" {
		t.Errorf("expected provider env-provider, got %s", settings.LLM.Provider)
	}
	if settings.LLM.APIKey != "env-api-key-456" {
		t.Errorf("expected api_key env-api-key-456, got %s", settings.LLM.APIKey)
	}
	if settings.LLM.Model != "qwen-plus" {
		t.Errorf("expected model qwen-plus, got %s", settings.LLM.Model)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		settings Settings
		wantErr bool
	}{
		{
			name: "valid LLM config",
			settings: Settings{
				LLM: LLMConfig{
					APIKey: "test-key",
				},
				Environment: EnvDevelopment,
			},
			wantErr: false,
		},
		{
			name: "valid legacy Anthropic config",
			settings: Settings{
				Agent: AgentConfig{
					AnthropicAPIKey: "anthropic-key",
				},
				Environment: EnvDevelopment,
			},
			wantErr: false,
		},
		{
			name: "no API key",
			settings: Settings{
				Environment: EnvDevelopment,
			},
			wantErr: true,
		},
		{
			name: "invalid environment",
			settings: Settings{
				LLM: LLMConfig{
					APIKey: "test-key",
				},
				Environment: Environment("invalid"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetDefaults(t *testing.T) {
	s := &Settings{}
	s.setDefaults()

	// Check defaults
	if s.Environment != EnvDevelopment {
		t.Errorf("expected default environment %s, got %s", EnvDevelopment, s.Environment)
	}
	if s.Server.Host != "0.0.0.0" {
		t.Errorf("expected default host 0.0.0.0, got %s", s.Server.Host)
	}
	if s.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", s.Server.Port)
	}
	if s.LLM.Provider != "dashscope" {
		t.Errorf("expected default LLM provider dashscope, got %s", s.LLM.Provider)
	}
	if s.LLM.Model != "qwen-plus" {
		t.Errorf("expected default LLM model qwen-plus, got %s", s.LLM.Model)
	}
	if s.LLM.MaxTokens != 4096 {
		t.Errorf("expected default MaxTokens 4096, got %d", s.LLM.MaxTokens)
	}
	if s.LLM.Temperature != 0.7 {
		t.Errorf("expected default Temperature 0.7, got %f", s.LLM.Temperature)
	}
	if s.Observability.LogLevel != "info" {
		t.Errorf("expected default log level info, got %s", s.Observability.LogLevel)
	}
}

func TestGetDataSourceConfig(t *testing.T) {
	s := &Settings{
		DataSources: map[string]interface{}{
			"prometheus": map[string]interface{}{
				"endpoint": "http://localhost:9090",
			},
		},
	}

	t.Run("existing datasource", func(t *testing.T) {
		config, err := s.GetDataSourceConfig("prometheus")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if config["endpoint"] != "http://localhost:9090" {
			t.Errorf("expected endpoint http://localhost:9090, got %v", config["endpoint"])
		}
	})

	t.Run("non-existing datasource", func(t *testing.T) {
		_, err := s.GetDataSourceConfig("nonexistent")
		if err == nil {
			t.Error("expected error for non-existent datasource, got nil")
		}
	})

	t.Run("no datasources configured", func(t *testing.T) {
		s2 := &Settings{}
		_, err := s2.GetDataSourceConfig("prometheus")
		if err == nil {
			t.Error("expected error when no datasources configured, got nil")
		}
	})
}
