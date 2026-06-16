// Package credentials provides credential management for data sources.
package credentials

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CredentialType represents the type of credential.
type CredentialType string

const (
	// CredentialTypeAccessKey represents access key credentials (e.g., Alibaba Cloud).
	CredentialTypeAccessKey CredentialType = "access_key"
	// CredentialTypeBasic represents basic auth credentials (username/password).
	CredentialTypeBasic CredentialType = "basic"
	// CredentialTypeBearer represents bearer token credentials.
	CredentialTypeBearer CredentialType = "bearer"
	// CredentialTypeSTSToken represents STS token credentials.
	CredentialTypeSTSToken CredentialType = "sts_token"
	// CredentialTypeIAM represents IAM role credentials.
	CredentialTypeIAM CredentialType = "iam"
)

// Credentials represents authentication credentials.
type Credentials struct {
	// Type is the type of credential.
	Type CredentialType `json:"type"`
	// AccessKeyID is the access key ID (for access_key type).
	AccessKeyID string `json:"access_key_id,omitempty"`
	// AccessKeySecret is the access key secret (for access_key type).
	AccessKeySecret string `json:"access_key_secret,omitempty"`
	// SecurityToken is the STS token (for sts_token type).
	SecurityToken string `json:"security_token,omitempty"`
	// Username is the username (for basic type).
	Username string `json:"username,omitempty"`
	// Password is the password (for basic type).
	Password string `json:"password,omitempty"`
	// BearerToken is the bearer token (for bearer type).
	BearerToken string `json:"bearer_token,omitempty"`
	// RoleARN is the IAM role ARN (for iam type).
	RoleARN string `json:"role_arn,omitempty"`
	// Expiry is the expiration time for temporary credentials.
	Expiry time.Time `json:"expiry,omitempty"`
	// Metadata contains additional credential metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// IsValid checks if the credentials are valid.
func (c *Credentials) IsValid() bool {
	switch c.Type {
	case CredentialTypeAccessKey, CredentialTypeSTSToken:
		return c.AccessKeyID != "" && c.AccessKeySecret != ""
	case CredentialTypeBasic:
		return c.Username != "" && c.Password != ""
	case CredentialTypeBearer:
		return c.BearerToken != ""
	case CredentialTypeIAM:
		return c.RoleARN != ""
	default:
		return false
	}
}

// IsExpired checks if the credentials have expired.
func (c *Credentials) IsExpired() bool {
	if c.Expiry.IsZero() {
		return false
	}
	return time.Now().After(c.Expiry)
}

// WillExpireIn checks if the credentials will expire within the given duration.
func (c *Credentials) WillExpireIn(duration time.Duration) bool {
	if c.Expiry.IsZero() {
		return false
	}
	return time.Now().Add(duration).After(c.Expiry)
}

// Provider is the interface for credential providers.
type Provider interface {
	// Get retrieves credentials for the given data source.
	Get(ctx context.Context, dataSource string) (*Credentials, error)

	// Refresh refreshes the credentials for the given data source.
	Refresh(ctx context.Context, dataSource string) (*Credentials, error)

	// GetWithRefresh retrieves credentials and refreshes if needed.
	GetWithRefresh(ctx context.Context, dataSource string, refreshBefore time.Duration) (*Credentials, error)
}

// StaticProvider is a provider that returns static credentials.
type StaticProvider struct {
	mu           sync.RWMutex
	credentials  map[string]*Credentials
}

// NewStaticProvider creates a new static credential provider.
func NewStaticProvider() *StaticProvider {
	return &StaticProvider{
		credentials: make(map[string]*Credentials),
	}
}

// Set sets the credentials for a data source.
func (p *StaticProvider) Set(dataSource string, creds *Credentials) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.credentials[dataSource] = creds
}

// Get retrieves credentials for the given data source.
func (p *StaticProvider) Get(ctx context.Context, dataSource string) (*Credentials, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	creds, exists := p.credentials[dataSource]
	if !exists {
		return nil, fmt.Errorf("credentials not found for data source: %s", dataSource)
	}

	if !creds.IsValid() {
		return nil, fmt.Errorf("invalid credentials for data source: %s", dataSource)
	}

	// Return a copy to avoid mutation
	return creds.copy(), nil
}

// Refresh is a no-op for static credentials.
func (p *StaticProvider) Refresh(ctx context.Context, dataSource string) (*Credentials, error) {
	return p.Get(ctx, dataSource)
}

// GetWithRefresh retrieves credentials, refreshing if needed.
func (p *StaticProvider) GetWithRefresh(ctx context.Context, dataSource string, refreshBefore time.Duration) (*Credentials, error) {
	creds, err := p.Get(ctx, dataSource)
	if err != nil {
		return nil, err
	}

	// Static credentials don't support refresh
	return creds, nil
}

// SetAccessKey sets access key credentials for a data source.
func (p *StaticProvider) SetAccessKey(dataSource, accessKeyID, accessKeySecret string) {
	p.Set(dataSource, &Credentials{
		Type:            CredentialTypeAccessKey,
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
	})
}

// SetSTSToken sets STS token credentials for a data source.
func (p *StaticProvider) SetSTSToken(dataSource, accessKeyID, accessKeySecret, securityToken string, expiry time.Time) {
	p.Set(dataSource, &Credentials{
		Type:            CredentialTypeSTSToken,
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
		SecurityToken:   securityToken,
		Expiry:          expiry,
	})
}

// SetBasic sets basic auth credentials for a data source.
func (p *StaticProvider) SetBasic(dataSource, username, password string) {
	p.Set(dataSource, &Credentials{
		Type:     CredentialTypeBasic,
		Username: username,
		Password: password,
	})
}

// SetBearer sets bearer token credentials for a data source.
func (p *StaticProvider) SetBearer(dataSource, token string, expiry time.Time) {
	p.Set(dataSource, &Credentials{
		Type:        CredentialTypeBearer,
		BearerToken: token,
		Expiry:      expiry,
	})
}

// Remove removes credentials for a data source.
func (p *StaticProvider) Remove(dataSource string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.credentials, dataSource)
}

// List returns all data source names that have credentials.
func (p *StaticProvider) List() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	names := make([]string, 0, len(p.credentials))
	for name := range p.credentials {
		names = append(names, name)
	}
	return names
}

// copy creates a copy of the credentials.
func (c *Credentials) copy() *Credentials {
	metadata := make(map[string]string)
	for k, v := range c.Metadata {
		metadata[k] = v
	}

	return &Credentials{
		Type:            c.Type,
		AccessKeyID:     c.AccessKeyID,
		AccessKeySecret: c.AccessKeySecret,
		SecurityToken:   c.SecurityToken,
		Username:        c.Username,
		Password:        c.Password,
		BearerToken:     c.BearerToken,
		RoleARN:         c.RoleARN,
		Expiry:          c.Expiry,
		Metadata:        metadata,
	}
}

// CacheProvider is a provider that caches credentials from another provider.
type CacheProvider struct {
	mu         sync.RWMutex
	provider   Provider
	cache      map[string]*cachedCredentials
	cacheTTL   time.Duration
}

type cachedCredentials struct {
	creds     *Credentials
	cachedAt  time.Time
}

// NewCacheProvider creates a new caching credential provider.
func NewCacheProvider(provider Provider, cacheTTL time.Duration) *CacheProvider {
	return &CacheProvider{
		provider: provider,
		cache:    make(map[string]*cachedCredentials),
		cacheTTL: cacheTTL,
	}
}

// Get retrieves credentials from cache or the underlying provider.
func (p *CacheProvider) Get(ctx context.Context, dataSource string) (*Credentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check cache
	if cached, exists := p.cache[dataSource]; exists {
		if time.Since(cached.cachedAt) < p.cacheTTL {
			return cached.creds.copy(), nil
		}
	}

	// Fetch from underlying provider
	creds, err := p.provider.Get(ctx, dataSource)
	if err != nil {
		return nil, err
	}

	// Update cache
	p.cache[dataSource] = &cachedCredentials{
		creds:    creds,
		cachedAt: time.Now(),
	}

	return creds.copy(), nil
}

// Refresh refreshes the credentials and updates the cache.
func (p *CacheProvider) Refresh(ctx context.Context, dataSource string) (*Credentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Refresh from underlying provider
	creds, err := p.provider.Refresh(ctx, dataSource)
	if err != nil {
		return nil, err
	}

	// Update cache
	p.cache[dataSource] = &cachedCredentials{
		creds:    creds,
		cachedAt: time.Now(),
	}

	return creds.copy(), nil
}

// GetWithRefresh retrieves credentials, refreshing if needed.
func (p *CacheProvider) GetWithRefresh(ctx context.Context, dataSource string, refreshBefore time.Duration) (*Credentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check cache
	if cached, exists := p.cache[dataSource]; exists {
		if time.Since(cached.cachedAt) < p.cacheTTL {
			// Check if refresh is needed
			if cached.creds.WillExpireIn(refreshBefore) {
				// Refresh
				creds, err := p.provider.Refresh(ctx, dataSource)
				if err != nil {
					// If refresh fails, return cached credentials if still valid
					if !cached.creds.IsExpired() {
						return cached.creds.copy(), nil
					}
					return nil, err
				}
				p.cache[dataSource] = &cachedCredentials{
					creds:    creds,
					cachedAt: time.Now(),
				}
				return creds.copy(), nil
			}
			return cached.creds.copy(), nil
		}
	}

	// Cache miss or expired, fetch fresh credentials
	creds, err := p.provider.Get(ctx, dataSource)
	if err != nil {
		return nil, err
	}

	p.cache[dataSource] = &cachedCredentials{
		creds:    creds,
		cachedAt: time.Now(),
	}

	return creds.copy(), nil
}

// Invalidate removes cached credentials for a data source.
func (p *CacheProvider) Invalidate(dataSource string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.cache, dataSource)
}

// InvalidateAll clears all cached credentials.
func (p *CacheProvider) InvalidateAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.cache = make(map[string]*cachedCredentials)
}
