// Package registry provides data source registration and discovery.
package registry

import (
	"context"
	"fmt"
	"sync"

	"github.com/yourusername/OpsAgent/internal/datasource/core"
	"github.com/yourusername/OpsAgent/internal/datasource/credentials"
	"github.com/yourusername/OpsAgent/internal/datasource/log"
	apm "github.com/yourusername/OpsAgent/internal/datasource/apm"
)

// DataSourceEntry holds a data source instance and its metadata.
type DataSourceEntry struct {
	DataSource core.DataSource
	Config     *core.Config
	Creds      *credentials.Credentials
}

// Registry manages data source registration and discovery.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*DataSourceEntry
	credProvider credentials.Provider
}

// NewRegistry creates a new data source registry.
func NewRegistry(credProvider credentials.Provider) *Registry {
	return &Registry{
		entries:     make(map[string]*DataSourceEntry),
		credProvider: credProvider,
	}
}

// Register registers a data source.
func (r *Registry) Register(name string, ds core.DataSource, config *core.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[name]; exists {
		return fmt.Errorf("data source %s already registered", name)
	}

	r.entries[name] = &DataSourceEntry{
		DataSource: ds,
		Config:     config,
	}
	return nil
}

// Get retrieves a data source by name.
func (r *Registry) Get(name string) (core.DataSource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.entries[name]
	if !exists {
		return nil, fmt.Errorf("data source %s not found", name)
	}
	return entry.DataSource, nil
}

// GetLogDataSource retrieves a log data source by name.
func (r *Registry) GetLogDataSource(name string) (core.LogDataSource, error) {
	ds, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	logDS, ok := ds.(core.LogDataSource)
	if !ok {
		return nil, fmt.Errorf("data source %s is not a log data source", name)
	}

	return logDS, nil
}

// GetAPMDataSource retrieves an APM data source by name.
func (r *Registry) GetAPMDataSource(name string) (core.APMDataSource, error) {
	ds, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	apmDS, ok := ds.(core.APMDataSource)
	if !ok {
		return nil, fmt.Errorf("data source %s is not an APM data source", name)
	}

	return apmDS, nil
}

// List returns all registered data source names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	return names
}

// ListByType returns data source names filtered by type.
func (r *Registry) ListByType(t core.DataSourceType) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0)
	for name, entry := range r.entries {
		if entry.DataSource.Type() == t {
			names = append(names, name)
		}
	}
	return names
}

// ListLogDataSources returns all log data source names.
func (r *Registry) ListLogDataSources() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0)
	for name, entry := range r.entries {
		if _, ok := entry.DataSource.(core.LogDataSource); ok {
			names = append(names, name)
		}
	}
	return names
}

// ListAPMDataSources returns all APM data source names.
func (r *Registry) ListAPMDataSources() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0)
	for name, entry := range r.entries {
		if _, ok := entry.DataSource.(core.APMDataSource); ok {
			names = append(names, name)
		}
	}
	return names
}

// ConnectAll connects all registered data sources.
func (r *Registry) ConnectAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, entry := range r.entries {
		if err := entry.DataSource.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect data source %s: %w", name, err)
		}
	}
	return nil
}

// CloseAll closes all registered data sources.
func (r *Registry) CloseAll() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var lastErr error
	for name, entry := range r.entries {
		if err := entry.DataSource.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close data source %s: %w", name, err)
		}
	}
	return lastErr
}

// Count returns the number of registered data sources.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// SetupFromConfig sets up data sources from configuration.
func (r *Registry) SetupFromConfig(ctx context.Context, configs map[string]interface{}) error {
	for name, cfg := range configs {
		cfgMap, ok := cfg.(map[string]interface{})
		if !ok {
			continue
		}

		dsType, ok := cfgMap["type"].(string)
		if !ok {
			continue
		}

		switch core.DataSourceType(dsType) {
		case core.DataSourceTypeSLS:
			slsConfig := r.parseSLSConfig(name, cfgMap)
			client, err := log.NewAliyunSLSClient(slsConfig)
			if err != nil {
				return fmt.Errorf("failed to create SLS client %s: %w", name, err)
			}
			if err := r.Register(name, client, &slsConfig.Config); err != nil {
				return err
			}
			if err := client.Connect(ctx); err != nil {
				return fmt.Errorf("failed to connect SLS %s: %w", name, err)
			}

		case core.DataSourceTypeARMS:
			armsConfig := r.parseARMSConfig(name, cfgMap)
			client, err := apm.NewAliyunARMSClient(armsConfig)
			if err != nil {
				return fmt.Errorf("failed to create ARMS client %s: %w", name, err)
			}
			if err := r.Register(name, client, &armsConfig.Config); err != nil {
				return err
			}
			if err := client.Connect(ctx); err != nil {
				return fmt.Errorf("failed to connect ARMS %s: %w", name, err)
			}

		default:
			// Unsupported data source type, skip
			continue
		}
	}
	return nil
}

// parseSLSConfig parses SLS configuration from a map.
func (r *Registry) parseSLSConfig(name string, cfg map[string]interface{}) *core.AliyunSLSConfig {
	config := &core.AliyunSLSConfig{
		Config: core.Config{
			Name:    name,
			Type:    core.DataSourceTypeSLS,
			Enabled: true,
		},
	}

	if v, ok := cfg["endpoint"].(string); ok {
		config.Endpoint = v
	}
	if v, ok := cfg["project"].(string); ok {
		config.Project = v
	}
	if v, ok := cfg["logstore"].(string); ok {
		config.LogStore = v
	}

	// Credentials from provider
	if r.credProvider != nil {
		creds, err := r.credProvider.Get(context.Background(), name)
		if err == nil && creds != nil {
			config.AccessKeyID = creds.AccessKeyID
			config.AccessKeySecret = creds.AccessKeySecret
			config.SecurityToken = creds.SecurityToken
		}
	}

	// Direct config overrides (for testing/dev)
	if v, ok := cfg["access_key_id"].(string); ok {
		config.AccessKeyID = v
	}
	if v, ok := cfg["access_key_secret"].(string); ok {
		config.AccessKeySecret = v
	}
	if v, ok := cfg["security_token"].(string); ok {
		config.SecurityToken = v
	}

	return config
}

// parseARMSConfig parses ARMS configuration from a map.
func (r *Registry) parseARMSConfig(name string, cfg map[string]interface{}) *core.AliyunARMSConfig {
	config := &core.AliyunARMSConfig{
		Config: core.Config{
			Name:    name,
			Type:    core.DataSourceTypeARMS,
			Enabled: true,
		},
	}

	if v, ok := cfg["endpoint"].(string); ok {
		config.Endpoint = v
	}
	if v, ok := cfg["region"].(string); ok {
		config.Region = v
	}
	if v, ok := cfg["pid"].(string); ok {
		config.Pid = v
	}

	// Credentials from provider
	if r.credProvider != nil {
		creds, err := r.credProvider.Get(context.Background(), name)
		if err == nil && creds != nil {
			config.AccessKeyID = creds.AccessKeyID
			config.AccessKeySecret = creds.AccessKeySecret
			config.SecurityToken = creds.SecurityToken
		}
	}

	// Direct config overrides
	if v, ok := cfg["access_key_id"].(string); ok {
		config.AccessKeyID = v
	}
	if v, ok := cfg["access_key_secret"].(string); ok {
		config.AccessKeySecret = v
	}
	if v, ok := cfg["security_token"].(string); ok {
		config.SecurityToken = v
	}

	return config
}
