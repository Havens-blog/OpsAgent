// Package core provides configuration structures for data sources.
package core

import (
	"fmt"
	"time"
)

// Config represents the base configuration for all data sources.
type Config struct {
	// Name is the identifier for this data source instance.
	Name string `json:"name" yaml:"name"`
	// Type is the type of the data source.
	Type DataSourceType `json:"type" yaml:"type"`
	// Enabled indicates if the data source is enabled.
	Enabled bool `json:"enabled" yaml:"enabled"`
	// Timeout is the default timeout for operations.
	Timeout time.Duration `json:"timeout" yaml:"timeout"`
	// RetryConfig contains retry configuration.
	RetryConfig *RetryConfig `json:"retry" yaml:"retry"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("data source name is required")
	}
	if c.Type == "" {
		return fmt.Errorf("data source type is required")
	}
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.RetryConfig == nil {
		c.RetryConfig = DefaultRetryConfig()
	}
	return nil
}

// RetryConfig contains retry configuration for data source operations.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts.
	MaxAttempts int `json:"max_attempts" yaml:"max_attempts"`
	// InitialBackoff is the initial backoff duration.
	InitialBackoff time.Duration `json:"initial_backoff" yaml:"initial_backoff"`
	// MaxBackoff is the maximum backoff duration.
	MaxBackoff time.Duration `json:"max_backoff" yaml:"max_backoff"`
	// BackoffMultiplier is the multiplier for backoff duration.
	BackoffMultiplier float64 `json:"backoff_multiplier" yaml:"backoff_multiplier"`
	// RetryableErrors is a list of error types that should be retried.
	RetryableErrors []string `json:"retryable_errors" yaml:"retryable_errors"`
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:      3,
		InitialBackoff:   500 * time.Millisecond,
		MaxBackoff:       30 * time.Second,
		BackoffMultiplier: 2.0,
		RetryableErrors:  []string{"timeout", "network", "rate_limit", "temporary"},
	}
}

// Validate validates the retry configuration.
func (c *RetryConfig) Validate() error {
	if c.MaxAttempts < 0 {
		return fmt.Errorf("max_attempts must be non-negative")
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 1
	}
	if c.InitialBackoff == 0 {
		c.InitialBackoff = 500 * time.Millisecond
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 30 * time.Second
	}
	if c.BackoffMultiplier == 0 {
		c.BackoffMultiplier = 2.0
	}
	if c.BackoffMultiplier < 1.0 {
		return fmt.Errorf("backoff_multiplier must be >= 1.0")
	}
	return nil
}

// ShouldRetry determines if an error should be retried based on its type.
func (c *RetryConfig) ShouldRetry(errorType string) bool {
	if errorType == "" {
		return true
	}
	for _, retryable := range c.RetryableErrors {
		if retryable == errorType || retryable == "all" {
			return true
		}
	}
	return false
}

// AliyunSLSConfig represents configuration for Alibaba Cloud SLS.
type AliyunSLSConfig struct {
	Config
	// Endpoint is the SLS endpoint (e.g., https://cn-hangzhou.log.aliyuncs.com).
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	// Project is the SLS project name.
	Project string `json:"project" yaml:"project"`
	// LogStore is the SLS log store name (optional, can be specified in queries).
	LogStore string `json:"logstore,omitempty" yaml:"logstore,omitempty"`
	// AccessKeyID is the Alibaba Cloud Access Key ID.
	AccessKeyID string `json:"access_key_id" yaml:"access_key_id"`
	// AccessKeySecret is the Alibaba Cloud Access Key Secret.
	AccessKeySecret string `json:"access_key_secret" yaml:"access_key_secret"`
	// SecurityToken is the STS token (optional).
	SecurityToken string `json:"security_token,omitempty" yaml:"security_token,omitempty"`
}

// Validate validates the SLS configuration.
func (c *AliyunSLSConfig) Validate() error {
	if err := c.Config.Validate(); err != nil {
		return err
	}
	if c.Endpoint == "" {
		return fmt.Errorf("SLS endpoint is required")
	}
	if c.Project == "" {
		return fmt.Errorf("SLS project is required")
	}
	if c.AccessKeyID == "" {
		return fmt.Errorf("access_key_id is required")
	}
	if c.AccessKeySecret == "" {
		return fmt.Errorf("access_key_secret is required")
	}
	return nil
}

// AliyunARMSConfig represents configuration for Alibaba Cloud ARMS.
type AliyunARMSConfig struct {
	Config
	// Endpoint is the ARMS endpoint.
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	// Region is the Alibaba Cloud region (e.g., cn-hangzhou).
	Region string `json:"region" yaml:"region"`
	// AccessKeyID is the Alibaba Cloud Access Key ID.
	AccessKeyID string `json:"access_key_id" yaml:"access_key_id"`
	// AccessKeySecret is the Alibaba Cloud Access Key Secret.
	AccessKeySecret string `json:"access_key_secret" yaml:"access_key_secret"`
	// SecurityToken is the STS token (optional).
	SecurityToken string `json:"security_token,omitempty" yaml:"security_token,omitempty"`
	// Pid is the ARMS application PID (optional, can be specified in queries).
	Pid string `json:"pid,omitempty" yaml:"pid,omitempty"`
}

// Validate validates the ARMS configuration.
func (c *AliyunARMSConfig) Validate() error {
	if err := c.Config.Validate(); err != nil {
		return err
	}
	if c.Region == "" {
		return fmt.Errorf("region is required")
	}
	if c.AccessKeyID == "" {
		return fmt.Errorf("access_key_id is required")
	}
	if c.AccessKeySecret == "" {
		return fmt.Errorf("access_key_secret is required")
	}
	return nil
}

// PrometheusConfig represents configuration for Prometheus.
type PrometheusConfig struct {
	Config
	// Endpoint is the Prometheus endpoint (e.g., http://prometheus:9090).
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	// Username is the basic auth username (optional).
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	// Password is the basic auth password (optional).
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
	// BearerToken is the bearer token for authentication (optional).
	BearerToken string `json:"bearer_token,omitempty" yaml:"bearer_token,omitempty"`
}

// Validate validates the Prometheus configuration.
func (c *PrometheusConfig) Validate() error {
	if err := c.Config.Validate(); err != nil {
		return err
	}
	if c.Endpoint == "" {
		return fmt.Errorf("Prometheus endpoint is required")
	}
	return nil
}

// ElasticsearchConfig represents configuration for Elasticsearch.
type ElasticsearchConfig struct {
	Config
	// Endpoints are the Elasticsearch endpoints.
	Endpoints []string `json:"endpoints" yaml:"endpoints"`
	// Username is the basic auth username (optional).
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	// Password is the basic auth password (optional).
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
	// IndexPattern is the default index pattern (optional).
	IndexPattern string `json:"index_pattern,omitempty" yaml:"index_pattern,omitempty"`
}

// Validate validates the Elasticsearch configuration.
func (c *ElasticsearchConfig) Validate() error {
	if err := c.Config.Validate(); err != nil {
		return err
	}
	if len(c.Endpoints) == 0 {
		return fmt.Errorf("at least one Elasticsearch endpoint is required")
	}
	return nil
}

// InfluxDBConfig represents configuration for InfluxDB.
type InfluxDBConfig struct {
	Config
	// Endpoint is the InfluxDB endpoint.
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	// Token is the authentication token.
	Token string `json:"token" yaml:"token"`
	// Org is the organization name.
	Org string `json:"org" yaml:"org"`
	// Bucket is the bucket name (optional, can be specified in queries).
	Bucket string `json:"bucket,omitempty" yaml:"bucket,omitempty"`
}

// Validate validates the InfluxDB configuration.
func (c *InfluxDBConfig) Validate() error {
	if err := c.Config.Validate(); err != nil {
		return err
	}
	if c.Endpoint == "" {
		return fmt.Errorf("InfluxDB endpoint is required")
	}
	if c.Token == "" {
		return fmt.Errorf("token is required")
	}
	if c.Org == "" {
		return fmt.Errorf("org is required")
	}
	return nil
}
