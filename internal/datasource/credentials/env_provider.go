// Package credentials provides environment variable credential provider.
package credentials

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EnvProvider is a credential provider that reads from environment variables.
type EnvProvider struct {
	mu             sync.RWMutex
	prefix         string
	credentialType map[string]CredentialType
}

// NewEnvProvider creates a new environment variable credential provider.
// The prefix is used to scope environment variables (e.g., "OPSAGENT").
func NewEnvProvider(prefix string) *EnvProvider {
	return &EnvProvider{
		prefix:         strings.ToUpper(prefix),
		credentialType: make(map[string]CredentialType),
	}
}

// RegisterCredentialsType registers the credential type for a data source.
func (p *EnvProvider) RegisterCredentialsType(dataSource string, credType CredentialType) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.credentialType[strings.ToUpper(dataSource)] = credType
}

// Get retrieves credentials for the given data source from environment variables.
func (p *EnvProvider) Get(ctx context.Context, dataSource string) (*Credentials, error) {
	upperDS := strings.ToUpper(dataSource)

	// Determine credential type
	credType := p.credentialType[upperDS]
	if credType == "" {
		// Try to infer from environment variable presence
		if p.getEnv(upperDS, "BEARER_TOKEN") != "" {
			credType = CredentialTypeBearer
		} else if p.getEnv(upperDS, "USERNAME") != "" {
			credType = CredentialTypeBasic
		} else {
			credType = CredentialTypeAccessKey
		}
	}

	var creds *Credentials

	switch credType {
	case CredentialTypeAccessKey, CredentialTypeSTSToken:
		accessKeyID := p.getEnv(upperDS, "ACCESS_KEY_ID")
		accessKeySecret := p.getEnv(upperDS, "ACCESS_KEY_SECRET")
		securityToken := p.getEnv(upperDS, "SECURITY_TOKEN")

		if accessKeyID == "" {
			return nil, fmt.Errorf("ACCESS_KEY_ID not found for data source: %s", dataSource)
		}
		if accessKeySecret == "" {
			return nil, fmt.Errorf("ACCESS_KEY_SECRET not found for data source: %s", dataSource)
		}

		creds = &Credentials{
			Type:            credType,
			AccessKeyID:     accessKeyID,
			AccessKeySecret: accessKeySecret,
			SecurityToken:   securityToken,
		}

		// Parse expiry if provided
		if expiryStr := p.getEnv(upperDS, "TOKEN_EXPIRY"); expiryStr != "" {
			if expiry, err := parseTimestamp(expiryStr); err == nil {
				creds.Expiry = expiry
			}
		}

	case CredentialTypeBasic:
		username := p.getEnv(upperDS, "USERNAME")
		password := p.getEnv(upperDS, "PASSWORD")

		if username == "" {
			return nil, fmt.Errorf("USERNAME not found for data source: %s", dataSource)
		}
		if password == "" {
			return nil, fmt.Errorf("PASSWORD not found for data source: %s", dataSource)
		}

		creds = &Credentials{
			Type:     CredentialTypeBasic,
			Username: username,
			Password: password,
		}

	case CredentialTypeBearer:
		token := p.getEnv(upperDS, "BEARER_TOKEN")

		if token == "" {
			return nil, fmt.Errorf("BEARER_TOKEN not found for data source: %s", dataSource)
		}

		creds = &Credentials{
			Type:        CredentialTypeBearer,
			BearerToken: token,
		}

		// Parse expiry if provided
		if expiryStr := p.getEnv(upperDS, "TOKEN_EXPIRY"); expiryStr != "" {
			if expiry, err := parseTimestamp(expiryStr); err == nil {
				creds.Expiry = expiry
			}
		}

	default:
		return nil, fmt.Errorf("unsupported credential type: %s", credType)
	}

	// Add metadata
	creds.Metadata = map[string]string{
		"provider": "environment",
		"source":   dataSource,
	}

	return creds, nil
}

// Refresh refreshes the credentials (re-reads from environment).
func (p *EnvProvider) Refresh(ctx context.Context, dataSource string) (*Credentials, error) {
	return p.Get(ctx, dataSource)
}

// GetWithRefresh retrieves credentials, refreshing if needed.
func (p *EnvProvider) GetWithRefresh(ctx context.Context, dataSource string, refreshBefore time.Duration) (*Credentials, error) {
	creds, err := p.Get(ctx, dataSource)
	if err != nil {
		return nil, err
	}

	// Environment credentials are static, but check expiry
	if creds.WillExpireIn(refreshBefore) {
		// For environment variables, we can't refresh
		return nil, fmt.Errorf("credentials are about to expire and cannot be refreshed")
	}

	return creds, nil
}

// getEnv retrieves an environment variable with the provider's prefix and data source.
// Format: {PREFIX}_{DATASOURCE}_{KEY}
func (p *EnvProvider) getEnv(dataSource, key string) string {
	envKey := fmt.Sprintf("%s_%s_%s", p.prefix, dataSource, key)
	return os.Getenv(envKey)
}

// parseTimestamp parses a timestamp string into a time.Time.
func parseTimestamp(s string) (time.Time, error) {
	// Try Unix timestamp (seconds)
	if sec, err := strconv.ParseInt(s, 10, 64); err == nil {
		// Check if it's in milliseconds (13 digits)
		if sec > 1000000000000 {
			return time.Unix(sec/1000, (sec%1000)*1000000), nil
		}
		return time.Unix(sec, 0), nil
	}

	// Try RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Try other common formats
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999Z",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", s)
}

// AliyunEnvProvider is a specialized provider for Alibaba Cloud credentials.
type AliyunEnvProvider struct {
	*EnvProvider
}

// NewAliyunEnvProvider creates a new Alibaba Cloud environment variable provider.
func NewAliyunEnvProvider(prefix string) *AliyunEnvProvider {
	return &AliyunEnvProvider{
		EnvProvider: NewEnvProvider(prefix),
	}
}

// Get retrieves Alibaba Cloud credentials from environment variables.
// Supports standard Aliyun env vars: ALIYUN_ACCESS_KEY_ID, ALIYUN_ACCESS_KEY_SECRET, ALIYUN_SECURITY_TOKEN
func (p *AliyunEnvProvider) Get(ctx context.Context, dataSource string) (*Credentials, error) {
	// Try standard Aliyun environment variables first
	accessKeyID := os.Getenv("ALIYUN_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("ALIYUN_ACCESS_KEY_SECRET")
	securityToken := os.Getenv("ALIYUN_SECURITY_TOKEN")

	if accessKeyID == "" {
		// Fall back to prefixed variables
		accessKeyID = p.getEnv("ALIYUN", "ACCESS_KEY_ID")
	}
	if accessKeySecret == "" {
		accessKeySecret = p.getEnv("ALIYUN", "ACCESS_KEY_SECRET")
	}
	if securityToken == "" {
		securityToken = p.getEnv("ALIYUN", "SECURITY_TOKEN")
	}

	if accessKeyID == "" {
		return nil, fmt.Errorf("ALIYUN_ACCESS_KEY_ID not found")
	}
	if accessKeySecret == "" {
		return nil, fmt.Errorf("ALIYUN_ACCESS_KEY_SECRET not found")
	}

	creds := &Credentials{
		Type:            CredentialTypeAccessKey,
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
	}

	if securityToken != "" {
		creds.Type = CredentialTypeSTSToken
		creds.SecurityToken = securityToken
	}

	// Add metadata
	creds.Metadata = map[string]string{
		"provider": "alibaba_cloud_env",
	}

	return creds, nil
}

// Refresh refreshes the credentials (re-reads from environment).
func (p *AliyunEnvProvider) Refresh(ctx context.Context, dataSource string) (*Credentials, error) {
	return p.Get(ctx, dataSource)
}

// GetWithRefresh retrieves credentials, refreshing if needed.
func (p *AliyunEnvProvider) GetWithRefresh(ctx context.Context, dataSource string, refreshBefore time.Duration) (*Credentials, error) {
	return p.EnvProvider.GetWithRefresh(ctx, dataSource, refreshBefore)
}

// ChainProvider chains multiple credential providers together.
type ChainProvider struct {
	providers []Provider
}

// NewChainProvider creates a new chain credential provider.
func NewChainProvider(providers ...Provider) *ChainProvider {
	return &ChainProvider{
		providers: providers,
	}
}

// Get retrieves credentials from the first provider that has them.
func (p *ChainProvider) Get(ctx context.Context, dataSource string) (*Credentials, error) {
	var lastErr error

	for _, provider := range p.providers {
		creds, err := provider.Get(ctx, dataSource)
		if err == nil {
			return creds, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("no provider in chain has credentials for %s: %w", dataSource, lastErr)
}

// Refresh refreshes credentials from the first provider that supports it.
func (p *ChainProvider) Refresh(ctx context.Context, dataSource string) (*Credentials, error) {
	var lastErr error

	for _, provider := range p.providers {
		creds, err := provider.Refresh(ctx, dataSource)
		if err == nil {
			return creds, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("no provider in chain can refresh credentials for %s: %w", dataSource, lastErr)
}

// GetWithRefresh retrieves credentials, refreshing if needed.
func (p *ChainProvider) GetWithRefresh(ctx context.Context, dataSource string, refreshBefore time.Duration) (*Credentials, error) {
	var lastErr error

	for _, provider := range p.providers {
		creds, err := provider.GetWithRefresh(ctx, dataSource, refreshBefore)
		if err == nil {
			return creds, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("no provider in chain has refreshable credentials for %s: %w", dataSource, lastErr)
}

// AddProvider adds a provider to the chain.
func (p *ChainProvider) AddProvider(provider Provider) {
	p.providers = append(p.providers, provider)
}

// Providers returns all providers in the chain.
func (p *ChainProvider) Providers() []Provider {
	return p.providers
}
