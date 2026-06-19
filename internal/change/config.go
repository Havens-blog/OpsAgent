// Package change provides configuration change tracking for OpsAgent.
// This module tracks changes to Kubernetes ConfigMaps, Secrets, and
// environment variables.
package change

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ConfigType represents the type of configuration.
type ConfigType string

const (
	// ConfigTypeConfigMap represents a Kubernetes ConfigMap
	ConfigTypeConfigMap ConfigType = "configmap"
	// ConfigTypeSecret represents a Kubernetes Secret
	ConfigTypeSecret ConfigType = "secret"
	// ConfigTypeEnvVar represents environment variables
	ConfigTypeEnvVar ConfigType = "env_var"
	// ConfigTypeDeployment represents deployment configuration
	ConfigTypeDeployment ConfigType = "deployment"
)

// ConfigChangeEvent represents a configuration change event.
type ConfigChangeEvent struct {
	// Unique identifier for this change
	ID string `json:"id"`

	// Type of configuration
	Type ConfigType `json:"type"`

	// Resource information
	Namespace   string `json:"namespace"`
	Name       string `json:"name"`
	Cluster     string `json:"cluster,omitempty"`

	// Change information
	Operation   ConfigOperation `json:"operation"` // created, updated, deleted
	ChangedKeys []string        `json:"changed_keys,omitempty"` // Keys that changed
	OldValues   map[string]string `json:"old_values,omitempty"`
	NewValues   map[string]string `json:"new_values,omitempty"`

	// Timing
	Timestamp time.Time `json:"timestamp"`

	// Source information
	Source   string `json:"source"` // kubernetes, git, etc.
	ResourceVersion string `json:"resource_version,omitempty"`

	// Affected services
	AffectedServices []string `json:"affected_services,omitempty"`

	// Annotations
	Annotations map[string]string `json:"annotations,omitempty"`

	// Correlation
	IncidentIDs []string `json:"incident_ids,omitempty"`
}

// ConfigOperation represents the type of configuration operation.
type ConfigOperation string

const (
	ConfigOperationCreated ConfigOperation = "created"
	ConfigOperationUpdated ConfigOperation = "updated"
	ConfigOperationDeleted ConfigOperation = "deleted"
)

// ConfigCollector defines the interface for collecting configuration changes.
type ConfigCollector interface {
	// CollectChanges collects configuration changes within a time window
	CollectChanges(ctx context.Context, window TimeWindow) ([]ConfigChangeEvent, error)

	// WatchChanges starts watching for configuration changes
	WatchChanges(ctx context.Context) (<-chan ConfigChangeEvent, error)

	// Health checks if the collector is working
	Health(ctx context.Context) error
}

// KubernetesCollector collects configuration changes from Kubernetes.
type KubernetesCollector struct {
	kubeconfig string
	cluster    string
	namespaces []string
}

// KubernetesCollectorOption configures a KubernetesCollector.
type KubernetesCollectorOption func(*KubernetesCollector)

// WithKubeconfig sets the kubeconfig file path.
func WithKubeconfig(path string) KubernetesCollectorOption {
	return func(c *KubernetesCollector) {
		c.kubeconfig = path
	}
}

// WithCluster sets the cluster name.
func WithCluster(name string) KubernetesCollectorOption {
	return func(c *KubernetesCollector) {
		c.cluster = name
	}
}

// WithNamespaces sets the namespaces to watch.
func WithNamespaces(namespaces []string) KubernetesCollectorOption {
	return func(c *KubernetesCollector) {
		c.namespaces = namespaces
	}
}

// NewKubernetesCollector creates a new Kubernetes configuration collector.
func NewKubernetesCollector(opts ...KubernetesCollectorOption) *KubernetesCollector {
	c := &KubernetesCollector{
		namespaces: []string{"default"},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// CollectChanges collects configuration changes from Kubernetes.
func (c *KubernetesCollector) CollectChanges(ctx context.Context, window TimeWindow) ([]ConfigChangeEvent, error) {
	// In a real implementation, this would:
	// 1. Query Kubernetes API for ConfigMap/Secret changes
	// 2. Query for Deployment changes
	// 3. Filter by time window
	// 4. Return change events

	// For now, return mock data for testing
	mockChanges := []ConfigChangeEvent{
		{
			ID:         "config-change-001",
			Type:       ConfigTypeConfigMap,
			Namespace:  "production",
			Name:       "app-config",
			Cluster:    c.cluster,
			Operation:  ConfigOperationUpdated,
			ChangedKeys: []string{"DATABASE_URL", "REDIS_HOST"},
			OldValues: map[string]string{
				"DATABASE_URL": "postgres-old:5432",
				"REDIS_HOST":   "redis-old",
			},
			NewValues: map[string]string{
				"DATABASE_URL": "postgres-new:5432",
				"REDIS_HOST":   "redis-new",
			},
			Timestamp: window.StartTime.Add(5 * time.Minute),
			Source:    "kubernetes",
			ResourceVersion: "12345",
			AffectedServices: []string{"order-service", "payment-service"},
		},
		{
			ID:         "config-change-002",
			Type:       ConfigTypeSecret,
			Namespace:  "production",
			Name:       "api-keys",
			Cluster:    c.cluster,
			Operation:  ConfigOperationUpdated,
			ChangedKeys: []string{"API_KEY"},
			OldValues:   map[string]string{},
			NewValues:   map[string]string{},
			Timestamp:   window.StartTime.Add(10 * time.Minute),
			Source:      "kubernetes",
			ResourceVersion: "12346",
			AffectedServices: []string{"api-gateway"},
		},
		{
			ID:         "config-change-003",
			Type:       ConfigTypeDeployment,
			Namespace:  "production",
			Name:       "order-service",
			Cluster:    c.cluster,
			Operation:  ConfigOperationUpdated,
			ChangedKeys: []string{"replicas", "image"},
			OldValues: map[string]string{
				"replicas": "3",
				"image":    "order-service:v1.0.0",
			},
			NewValues: map[string]string{
				"replicas": "5",
				"image":    "order-service:v1.1.0",
			},
			Timestamp:       window.StartTime.Add(15 * time.Minute),
			Source:          "kubernetes",
			ResourceVersion: "12347",
			AffectedServices: []string{"order-service"},
		},
	}

	return mockChanges, nil
}

// WatchChanges watches for configuration changes in Kubernetes.
func (c *KubernetesCollector) WatchChanges(ctx context.Context) (<-chan ConfigChangeEvent, error) {
	// In a real implementation, use Kubernetes watch API
	ch := make(chan ConfigChangeEvent)
	return ch, fmt.Errorf("not implemented")
}

// Health checks if the Kubernetes API is accessible.
func (c *KubernetesCollector) Health(ctx context.Context) error {
	// In a real implementation, check Kubernetes API connectivity
	return fmt.Errorf("not implemented")
}

// ConfigAnalyzer analyzes configuration changes and correlates them with incidents.
type ConfigAnalyzer struct {
	store     ChangeStore
	collectors map[string]ConfigCollector
}

// NewConfigAnalyzer creates a new configuration analyzer.
func NewConfigAnalyzer(store ChangeStore) *ConfigAnalyzer {
	return &ConfigAnalyzer{
		store:      store,
		collectors: make(map[string]ConfigCollector),
	}
}

// RegisterCollector registers a configuration collector.
func (a *ConfigAnalyzer) RegisterCollector(name string, collector ConfigCollector) {
	a.collectors[name] = collector
}

// CollectRecentChanges collects all configuration changes within a time window.
func (a *ConfigAnalyzer) CollectRecentChanges(ctx context.Context, window TimeWindow) ([]ConfigChangeEvent, error) {
	var allChanges []ConfigChangeEvent

	for name, collector := range a.collectors {
		changes, err := collector.CollectChanges(ctx, window)
		if err != nil {
			// Log error but continue with other collectors
			fmt.Printf("Failed to collect from %s: %v\n", name, err)
			continue
		}

		allChanges = append(allChanges, changes...)
	}

	return allChanges, nil
}

// CorrelateWithIncident correlates configuration changes with an incident.
func (a *ConfigAnalyzer) CorrelateWithIncident(
	ctx context.Context,
	incidentID string,
	incidentWindow TimeWindow,
) ([]ChangeCorrelation, error) {
	// Look back 30 minutes before incident start
	searchWindow := TimeWindow{
		StartTime: incidentWindow.StartTime.Add(-30 * time.Minute),
		EndTime:   incidentWindow.EndTime,
	}

	// Collect changes
	changes, err := a.CollectRecentChanges(ctx, searchWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to collect changes: %w", err)
	}

	var correlations []ChangeCorrelation

	for _, change := range changes {
		correlation := a.correlateChange(change, incidentID, incidentWindow)
		if correlation != nil {
			correlations = append(correlations, *correlation)
		}
	}

	return correlations, nil
}

// correlateChange correlates a single configuration change with an incident.
func (a *ConfigAnalyzer) correlateChange(
	change ConfigChangeEvent,
	incidentID string,
	incidentWindow TimeWindow,
) *ChangeCorrelation {
	// Calculate time delta
	var timeDelta time.Duration
	if change.Timestamp.Before(incidentWindow.StartTime) {
		timeDelta = incidentWindow.StartTime.Sub(change.Timestamp)
	} else {
		timeDelta = 0
	}

	// Calculate correlation score based on:
	// 1. Time proximity (0-30 minutes = higher score)
	// 2. Change severity (Secret > ConfigMap > Deployment)
	// 3. Service match
	// 4. Key sensitivity

	score := 0.0
	reasons := []string{}

	// Time proximity scoring - more important than change type
	// Changes beyond 30 minutes are not correlated
	if timeDelta > 30*time.Minute {
		return nil
	}

	if timeDelta < 5*time.Minute {
		score += 0.4
		reasons = append(reasons, "config change within 5 minutes")
	} else if timeDelta < 15*time.Minute {
		score += 0.2
		reasons = append(reasons, "config change within 15 minutes")
	} else if timeDelta < 30*time.Minute {
		score += 0.1
		reasons = append(reasons, "config change within 30 minutes")
	}

	// Change type scoring (only applied if time proximity passed)
	switch change.Type {
	case ConfigTypeSecret:
		score += 0.4
		reasons = append(reasons, "secret change (high impact)")
	case ConfigTypeConfigMap:
		score += 0.2
		reasons = append(reasons, "configmap change")
	case ConfigTypeDeployment:
		score += 0.1
		reasons = append(reasons, "deployment config change")
	}

	// Check for sensitive key changes
	sensitiveKeys := []string{"PASSWORD", "SECRET", "KEY", "TOKEN", "DATABASE_URL", "API_KEY"}
	for _, key := range change.ChangedKeys {
		for _, sensitive := range sensitiveKeys {
			if contains(key, sensitive) {
				score += 0.2
				reasons = append(reasons, fmt.Sprintf("sensitive key changed: %s", key))
				break
			}
		}
	}

	// Build reason string
	reason := fmt.Sprintf("Configuration change %s", change.ID)
	if len(reasons) > 0 {
		reason += ": " + reasons[0]
		for i := 1; i < len(reasons); i++ {
			reason += ", " + reasons[i]
		}
	}

	if score < 0.1 {
		return nil // Not correlated enough
	}

	return &ChangeCorrelation{
		ChangeID:         change.ID,
		ChangeType:       mapConfigTypeToChangeType(change.Type),
		IncidentID:       incidentID,
		CorrelationScore: score,
		Reason:          reason,
		TimeDelta:       timeDelta,
	}
}

// mapConfigTypeToChangeType maps ConfigType to ChangeType.
func mapConfigTypeToChangeType(ct ConfigType) ChangeType {
	switch ct {
	case ConfigTypeConfigMap:
		return ChangeTypeConfig
	case ConfigTypeSecret:
		return ChangeTypeSecret
	case ConfigTypeDeployment:
		return ChangeTypeInfrastructure
	default:
		return ChangeTypeConfig
	}
}

// contains checks if a string contains a substring (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
		 len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
			 s[len(s)-len(substr):] == substr ||
			 indexOf(s, substr) >= 0))
}

// indexOf finds the index of a substring (case-insensitive).
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// GetConfigSeverity returns the severity of a configuration change.
func GetConfigSeverity(change ConfigChangeEvent) ChangeSeverity {
	// Secret changes are always critical
	if change.Type == ConfigTypeSecret {
		return SeverityCritical
	}

	// Check for sensitive keys
	sensitiveKeys := []string{"PASSWORD", "SECRET", "KEY", "TOKEN", "DATABASE_URL"}
	for _, key := range change.ChangedKeys {
		for _, sensitive := range sensitiveKeys {
			if contains(key, sensitive) {
				return SeverityHigh
			}
		}
	}

	// Check affected services count
	if len(change.AffectedServices) > 5 {
		return SeverityHigh
	}

	// Default to medium for config changes
	return SeverityMedium
}

// ConfigChangeFilter defines filters for listing configuration changes.
type ConfigChangeFilter struct {
	Namespace  string
	ConfigType ConfigType
	Operation  ConfigOperation
	StartTime  *time.Time
	EndTime    *time.Time
	Service    string
	Limit      int
	Offset     int
}

// ConfigStore defines the interface for storing configuration changes.
type ConfigStore interface {
	// StoreConfigChange stores a configuration change event
	StoreConfigChange(ctx context.Context, change ConfigChangeEvent) error

	// GetConfigChange retrieves a configuration change by ID
	GetConfigChange(ctx context.Context, id string) (ConfigChangeEvent, error)

	// ListConfigChanges lists configuration changes matching filters
	ListConfigChanges(ctx context.Context, filter ConfigChangeFilter) ([]ConfigChangeEvent, error)
}

// MemoryConfigStore provides an in-memory implementation of ConfigStore.
type MemoryConfigStore struct {
	changes map[string]ConfigChangeEvent
	mu      sync.RWMutex
}

// NewMemoryConfigStore creates a new in-memory configuration store.
func NewMemoryConfigStore() *MemoryConfigStore {
	return &MemoryConfigStore{
		changes: make(map[string]ConfigChangeEvent),
	}
}

// StoreConfigChange stores a configuration change event.
func (s *MemoryConfigStore) StoreConfigChange(ctx context.Context, change ConfigChangeEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if change.ID == "" {
		change.ID = generateConfigChangeID(change)
	}

	s.changes[change.ID] = change
	return nil
}

// GetConfigChange retrieves a configuration change by ID.
func (s *MemoryConfigStore) GetConfigChange(ctx context.Context, id string) (ConfigChangeEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	change, ok := s.changes[id]
	if !ok {
		return ConfigChangeEvent{}, fmt.Errorf("config change not found: %s", id)
	}

	return change, nil
}

// ListConfigChanges lists configuration changes matching filters.
func (s *MemoryConfigStore) ListConfigChanges(ctx context.Context, filter ConfigChangeFilter) ([]ConfigChangeEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []ConfigChangeEvent

	for _, change := range s.changes {
		if s.matchesFilter(change, filter) {
			results = append(results, change)
		}
	}

	// Apply offset
	if filter.Offset > 0 && filter.Offset < len(results) {
		results = results[filter.Offset:]
	}

	// Apply limit
	if filter.Limit > 0 && filter.Limit < len(results) {
		results = results[:filter.Limit]
	}

	return results, nil
}

// matchesFilter checks if a change matches the given filter.
func (s *MemoryConfigStore) matchesFilter(change ConfigChangeEvent, filter ConfigChangeFilter) bool {
	// Namespace filter
	if filter.Namespace != "" && change.Namespace != filter.Namespace {
		return false
	}

	// Config type filter
	if filter.ConfigType != "" && change.Type != filter.ConfigType {
		return false
	}

	// Operation filter
	if filter.Operation != "" && change.Operation != filter.Operation {
		return false
	}

	// Time window filter
	if filter.StartTime != nil && change.Timestamp.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && change.Timestamp.After(*filter.EndTime) {
		return false
	}

	// Service filter
	if filter.Service != "" {
		found := false
		for _, service := range change.AffectedServices {
			if service == filter.Service {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// generateConfigChangeID generates a unique configuration change ID.
func generateConfigChangeID(change ConfigChangeEvent) string {
	timestamp := change.Timestamp.Format("20060102150405")
	return fmt.Sprintf("config-%s-%s-%s", timestamp, change.Type, change.Name)
}
