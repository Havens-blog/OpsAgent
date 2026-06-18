// Package config provides configuration management for OpsAgent.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Environment represents the deployment environment.
type Environment string

const (
	// EnvDevelopment represents the development environment.
	EnvDevelopment Environment = "development"
	// EnvStaging represents the staging environment.
	EnvStaging Environment = "staging"
	// EnvProduction represents the production environment.
	EnvProduction Environment = "production"
)

// Settings holds the application configuration.
type Settings struct {
	mu sync.RWMutex

	// Environment is the deployment environment
	Environment Environment `yaml:"environment" env:"OPSAGENT_ENVIRONMENT"`

	// Server configuration
	Server ServerConfig `yaml:"server"`

	// Agent configuration
	Agent AgentConfig `yaml:"agent"`

	// LLM configuration
	LLM LLMConfig `yaml:"llm"`

	// Data sources configuration
	DataSources map[string]interface{} `yaml:"datasources"`

	// Observability configuration
	Observability ObservabilityConfig `yaml:"observability"`

	// Security configuration
	Security SecurityConfig `yaml:"security"`

	// DingTalk configuration
	DingTalk DingTalkConfig `yaml:"dingtalk"`

	// Paths
	paths ConfigPaths
}

// ServerConfig holds server-related configuration.
type ServerConfig struct {
	// Host is the server host
	Host string `yaml:"host" env:"OPSAGENT_HOST"`
	// Port is the server port
	Port int `yaml:"port" env:"OPSAGENT_PORT"`
	// ReadTimeout is the read timeout in seconds
	ReadTimeout int `yaml:"read_timeout" env:"OPSAGENT_READ_TIMEOUT"`
	// WriteTimeout is the write timeout in seconds
	WriteTimeout int `yaml:"write_timeout" env:"OPSAGENT_WRITE_TIMEOUT"`
}

// AgentConfig holds agent-related configuration.
type AgentConfig struct {
	// AnthropicAPIKey is the API key for Anthropic (legacy, prefer LLMConfig)
	AnthropicAPIKey string `yaml:"anthropic_api_key" env:"ANTHROPIC_API_KEY"`
	// Model is the model to use
	Model string `yaml:"model" env:"OPSAGENT_MODEL"`
	// MaxTurns is the maximum number of turns per conversation
	MaxTurns int `yaml:"max_turns" env:"OPSAGENT_MAX_TURNS"`
	// Timeout is the agent timeout in seconds
	Timeout int `yaml:"timeout" env:"OPSAGENT_AGENT_TIMEOUT"`
	// MaxParallelAgents is the maximum number of parallel agents
	MaxParallelAgents int `yaml:"max_parallel_agents" env:"OPSAGENT_MAX_PARALLEL_AGENTS"`
}

// LLMConfig holds LLM provider configuration.
type LLMConfig struct {
	// Provider is the LLM provider (claude, openai, dashscope, volcengine, etc.)
	Provider string `yaml:"provider" env:"LLM_PROVIDER"`
	// BaseURL is the API base URL
	BaseURL string `yaml:"base_url" env:"LLM_BASE_URL"`
	// APIKey is the API key for the LLM provider
	APIKey string `yaml:"api_key" env:"LLM_API_KEY"`
	// Model is the model name to use
	Model string `yaml:"model" env:"LLM_MODEL"`
	// MaxTokens is the maximum tokens in response
	MaxTokens int `yaml:"max_tokens" env:"LLM_MAX_TOKENS"`
	// Temperature is the sampling temperature
	Temperature float64 `yaml:"temperature" env:"LLM_TEMPERATURE"`
	// Timeout is the request timeout in seconds
	Timeout int `yaml:"timeout" env:"LLM_TIMEOUT"`
}

// ObservabilityConfig holds observability configuration.
type ObservabilityConfig struct {
	// LogLevel is the log level (debug, info, warn, error)
	LogLevel string `yaml:"log_level" env:"OPSAGENT_LOG_LEVEL"`
	// LogFormat is the log format (json, text)
	LogFormat string `yaml:"log_format" env:"OPSAGENT_LOG_FORMAT"`
	// EnableTracing enables distributed tracing
	EnableTracing bool `yaml:"enable_tracing" env:"OPSAGENT_ENABLE_TRACING"`
	// OTLPEndpoint is the OTLP endpoint for tracing
	OTLPEndpoint string `yaml:"otlp_endpoint" env:"OPSAGENT_OTLP_ENDPOINT"`
	// EnableMetrics enables metrics collection
	EnableMetrics bool `yaml:"enable_metrics" env:"OPSAGENT_ENABLE_METRICS"`
	// MetricsPort is the port for metrics endpoint
	MetricsPort int `yaml:"metrics_port" env:"OPSAGENT_METRICS_PORT"`
}

// SecurityConfig holds security configuration.
type SecurityConfig struct {
	// EnableAuditLog enables audit logging
	EnableAuditLog bool `yaml:"enable_audit_log" env:"OPSAGENT_ENABLE_AUDIT_LOG"`
	// AuditLogPath is the path to the audit log file
	AuditLogPath string `yaml:"audit_log_path" env:"OPSAGENT_AUDIT_LOG_PATH"`
	// VaultAddr is the Vault address
	VaultAddr string `yaml:"vault_addr" env:"VAULT_ADDR"`
	// VaultToken is the Vault token
	VaultToken string `yaml:"vault_token" env:"VAULT_TOKEN"`
	// VaultPath is the base path for secrets in Vault
	VaultPath string `yaml:"vault_path" env:"OPSAGENT_VAULT_PATH"`
}

// DingTalkConfig holds DingTalk notification configuration.
type DingTalkConfig struct {
	// WebhookURL is the DingTalk webhook URL
	WebhookURL string `yaml:"webhook_url" env:"DINGTALK_WEBHOOK_URL"`
	// Secret is the DingTalk webhook secret
	Secret string `yaml:"secret" env:"DINGTALK_SECRET"`
}

// ConfigPaths holds configuration file paths.
type ConfigPaths struct {
	// ConfigDir is the configuration directory
	ConfigDir string
	// SettingsFile is the main settings file
	SettingsFile string
	// AgentsFile is the agents configuration file
	AgentsFile string
	// DataSourcesFile is the datasources configuration file
	DataSourcesFile string
}

// Global settings instance
var (
	globalSettings *Settings
	once          sync.Once
)

// Load loads the configuration from file and environment variables.
func Load(configDir string) (*Settings, error) {
	once.Do(func() {
		globalSettings = &Settings{
			Environment: EnvDevelopment,
			paths: ConfigPaths{
				ConfigDir:        configDir,
				SettingsFile:     filepath.Join(configDir, "settings.yaml"),
				AgentsFile:       filepath.Join(configDir, "agents.yaml"),
				DataSourcesFile:  filepath.Join(configDir, "datasources.yaml"),
			},
		}
	})

	// Load from YAML file if it exists
	if _, err := os.Stat(globalSettings.paths.SettingsFile); err == nil {
		if err := globalSettings.loadFromFile(globalSettings.paths.SettingsFile); err != nil {
			return nil, fmt.Errorf("failed to load settings from file: %w", err)
		}
	}

	// Load from environment variables
	globalSettings.loadFromEnv()

	// Set defaults
	globalSettings.setDefaults()

	// Validate
	if err := globalSettings.Validate(); err != nil {
		return nil, fmt.Errorf("invalid settings: %w", err)
	}

	return globalSettings, nil
}

// loadFromFile loads configuration from a YAML file.
func (s *Settings) loadFromFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Expand environment variables in the YAML content
	expandedData := expandEnv(string(data))

	if err := yaml.Unmarshal([]byte(expandedData), s); err != nil {
		return err
	}

	// Expand environment variables in specific fields
	s.LLM.APIKey = expandEnv(s.LLM.APIKey)
	s.DingTalk.WebhookURL = expandEnv(s.DingTalk.WebhookURL)
	s.DingTalk.Secret = expandEnv(s.DingTalk.Secret)

	return nil
}

// expandEnv expands environment variables in the form ${VAR} or $VAR.
func expandEnv(s string) string {
	// Handle ${VAR} syntax
	s = os.ExpandEnv(s)
	// Handle $VAR syntax is already handled by ExpandEnv
	return s
}

// loadFromEnv loads configuration from environment variables.
func (s *Settings) loadFromEnv() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Environment
	if env := os.Getenv("OPSAGENT_ENVIRONMENT"); env != "" {
		s.Environment = Environment(env)
	}

	// Server
	if host := os.Getenv("OPSAGENT_HOST"); host != "" {
		s.Server.Host = host
	}
	if port := os.Getenv("OPSAGENT_PORT"); port != "" {
		fmt.Sscanf(port, "%d", &s.Server.Port)
	}

	// Agent
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		s.Agent.AnthropicAPIKey = apiKey
	}
	if model := os.Getenv("OPSAGENT_MODEL"); model != "" {
		s.Agent.Model = model
	}

	// LLM
	if provider := os.Getenv("LLM_PROVIDER"); provider != "" {
		s.LLM.Provider = provider
	}
	if baseURL := os.Getenv("LLM_BASE_URL"); baseURL != "" {
		s.LLM.BaseURL = baseURL
	}
	if apiKey := os.Getenv("LLM_API_KEY"); apiKey != "" {
		s.LLM.APIKey = apiKey
	}
	if model := os.Getenv("LLM_MODEL"); model != "" {
		s.LLM.Model = model
	}
	if maxTokens := os.Getenv("LLM_MAX_TOKENS"); maxTokens != "" {
		fmt.Sscanf(maxTokens, "%d", &s.LLM.MaxTokens)
	}
	if temperature := os.Getenv("LLM_TEMPERATURE"); temperature != "" {
		fmt.Sscanf(temperature, "%f", &s.LLM.Temperature)
	}
	if timeout := os.Getenv("LLM_TIMEOUT"); timeout != "" {
		fmt.Sscanf(timeout, "%d", &s.LLM.Timeout)
	}

	// Observability
	if logLevel := os.Getenv("OPSAGENT_LOG_LEVEL"); logLevel != "" {
		s.Observability.LogLevel = logLevel
	}
	if otlpEndpoint := os.Getenv("OPSAGENT_OTLP_ENDPOINT"); otlpEndpoint != "" {
		s.Observability.OTLPEndpoint = otlpEndpoint
	}

	// Security
	if enableAudit := os.Getenv("OPSAGENT_ENABLE_AUDIT_LOG"); enableAudit != "" {
		s.Security.EnableAuditLog = strings.ToLower(enableAudit) == "true"
	}

	// DingTalk
	if webhookURL := os.Getenv("DINGTALK_WEBHOOK_URL"); webhookURL != "" {
		s.DingTalk.WebhookURL = webhookURL
	}
	if secret := os.Getenv("DINGTALK_SECRET"); secret != "" {
		s.DingTalk.Secret = secret
	}
}

// setDefaults sets default values for configuration.
func (s *Settings) setDefaults() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Environment == "" {
		s.Environment = EnvDevelopment
	}

	if s.Server.Host == "" {
		s.Server.Host = "0.0.0.0"
	}
	if s.Server.Port == 0 {
		s.Server.Port = 8080
	}
	if s.Server.ReadTimeout == 0 {
		s.Server.ReadTimeout = 30
	}
	if s.Server.WriteTimeout == 0 {
		s.Server.WriteTimeout = 30
	}

	if s.Agent.Model == "" {
		s.Agent.Model = "claude-sonnet-4-20250514"
	}
	if s.Agent.MaxTurns == 0 {
		s.Agent.MaxTurns = 10
	}
	if s.Agent.Timeout == 0 {
		s.Agent.Timeout = 300
	}
	if s.Agent.MaxParallelAgents == 0 {
		s.Agent.MaxParallelAgents = 5
	}

	// LLM defaults - use Claude by default
	if s.LLM.Provider == "" {
		s.LLM.Provider = "claude"
	}
	if s.LLM.Model == "" {
		s.LLM.Model = "claude-sonnet-4-20250514"
	}
	if s.LLM.MaxTokens == 0 {
		s.LLM.MaxTokens = 4096
	}
	if s.LLM.Temperature == 0 {
		s.LLM.Temperature = 0.7
	}
	if s.LLM.Timeout == 0 {
		s.LLM.Timeout = 120
	}

	if s.Observability.LogLevel == "" {
		s.Observability.LogLevel = "info"
	}
	if s.Observability.LogFormat == "" {
		s.Observability.LogFormat = "json"
	}
	if s.Observability.MetricsPort == 0 {
		s.Observability.MetricsPort = 9090
	}
}

// Validate validates the configuration.
func (s *Settings) Validate() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check API key: either legacy ANTHROPIC_API_KEY or new LLM.APIKey
	hasAPIKey := s.Agent.AnthropicAPIKey != "" || s.LLM.APIKey != ""
	if !hasAPIKey {
		return fmt.Errorf("either ANTHROPIC_API_KEY or LLM_API_KEY is required")
	}

	// If using legacy config, migrate to LLM config
	if s.Agent.AnthropicAPIKey != "" && s.LLM.APIKey == "" {
		s.LLM.APIKey = s.Agent.AnthropicAPIKey
		if s.LLM.Provider == "" {
			s.LLM.Provider = "claude"
		}
		if s.LLM.Model == "" {
			s.LLM.Model = s.Agent.Model
		}
	}

	switch s.Environment {
	case EnvDevelopment, EnvStaging, EnvProduction:
		// Valid
	default:
		return fmt.Errorf("invalid environment: %s", s.Environment)
	}

	return nil
}

// Get returns the global settings instance.
func Get() *Settings {
	if globalSettings == nil {
		// Initialize with defaults
		globalSettings = &Settings{
			Environment: EnvDevelopment,
		}
		globalSettings.setDefaults()
	}
	return globalSettings
}

// GetDataSourceConfig returns the configuration for a specific data source.
func (s *Settings) GetDataSourceConfig(source string) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.DataSources == nil {
		return nil, fmt.Errorf("no datasources configured")
	}

	config, exists := s.DataSources[source]
	if !exists {
		return nil, fmt.Errorf("datasource %s not found", source)
	}

	configMap, ok := config.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid datasource configuration for %s", source)
	}

	return configMap, nil
}

// Save saves the configuration to a YAML file.
func (s *Settings) Save(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// ToLLMConfig converts the LLM configuration to llm.Config.
func (s *Settings) ToLLMConfig() LLMConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.LLM
}
