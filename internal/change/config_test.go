// Package change provides tests for configuration change tracking.
package change

import (
	"context"
	"testing"
	"time"
)

func TestMemoryConfigStore(t *testing.T) {
	store := NewMemoryConfigStore()
	ctx := context.Background()

	t.Run("Store and Get Config Change", func(t *testing.T) {
		change := ConfigChangeEvent{
			ID:         "config-change-001",
			Type:       ConfigTypeConfigMap,
			Namespace:  "production",
			Name:       "app-config",
			Operation:  ConfigOperationUpdated,
			ChangedKeys: []string{"DATABASE_URL"},
			Timestamp:  time.Now(),
			Source:     "kubernetes",
		}

		err := store.StoreConfigChange(ctx, change)
		if err != nil {
			t.Fatalf("Failed to store config change: %v", err)
		}

		retrieved, err := store.GetConfigChange(ctx, "config-change-001")
		if err != nil {
			t.Fatalf("Failed to get config change: %v", err)
		}

		if retrieved.ID != change.ID {
			t.Errorf("Expected ID %s, got %s", change.ID, retrieved.ID)
		}

		if retrieved.Name != change.Name {
			t.Errorf("Expected name %s, got %s", change.Name, retrieved.Name)
		}
	})

	t.Run("List Config Changes with Filters", func(t *testing.T) {
		// Store multiple changes
		changes := []ConfigChangeEvent{
			{
				ID:         "config-001",
				Type:       ConfigTypeConfigMap,
				Namespace:  "production",
				Name:       "config-a",
				Operation:  ConfigOperationUpdated,
				Timestamp:  time.Now().Add(-1 * time.Hour),
			},
			{
				ID:         "config-002",
				Type:       ConfigTypeSecret,
				Namespace:  "production",
				Name:       "secret-b",
				Operation:  ConfigOperationUpdated,
				Timestamp:  time.Now().Add(-30 * time.Minute),
			},
			{
				ID:         "config-003",
				Type:       ConfigTypeConfigMap,
				Namespace:  "staging",
				Name:       "config-c",
				Operation:  ConfigOperationCreated,
				Timestamp:  time.Now().Add(-15 * time.Minute),
			},
		}

		for _, c := range changes {
			if err := store.StoreConfigChange(ctx, c); err != nil {
				t.Fatalf("Failed to store config change: %v", err)
			}
		}

		// Filter by namespace
		filter := ConfigChangeFilter{
			Namespace: "production",
		}

		results, err := store.ListConfigChanges(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to list config changes: %v", err)
		}

		if len(results) != 3 { // 1 from first test + 2 from this test
			t.Errorf("Expected 3 config changes for production namespace, got %d", len(results))
		}

		// Filter by type
		filter = ConfigChangeFilter{
			ConfigType: ConfigTypeSecret,
		}

		results, err = store.ListConfigChanges(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to list config changes: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("Expected 1 secret change, got %d", len(results))
		}
	})
}

func TestConfigAnalyzer(t *testing.T) {
	t.Run("Correlate Secret Change with Incident", func(t *testing.T) {
		analyzer := NewConfigAnalyzer(nil)

		// Create a secret change 2 minutes before incident
		change := ConfigChangeEvent{
			ID:         "secret-change-001",
			Type:       ConfigTypeSecret,
			Namespace:  "production",
			Name:       "api-secret",
			Operation:  ConfigOperationUpdated,
			ChangedKeys: []string{"API_KEY"},
			Timestamp:  time.Now().Add(-2 * time.Minute),
			Source:     "kubernetes",
		}

		correlation := analyzer.correlateChange(change, "incident-001", TimeWindow{
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now(),
		})

		if correlation == nil {
			t.Fatal("Expected correlation, got nil")
		}

		// Secret change should have high correlation score
		if correlation.CorrelationScore < 0.7 {
			t.Errorf("Secret change should have correlation score >= 0.7, got %f", correlation.CorrelationScore)
		}

		if correlation.ChangeType != ChangeTypeSecret {
			t.Errorf("Expected change type %s, got %s", ChangeTypeSecret, correlation.ChangeType)
		}
	})

	t.Run("Correlate ConfigMap Change with Incident", func(t *testing.T) {
		analyzer := NewConfigAnalyzer(nil)

		change := ConfigChangeEvent{
			ID:         "configmap-change-001",
			Type:       ConfigTypeConfigMap,
			Namespace:  "production",
			Name:       "app-config",
			Operation:  ConfigOperationUpdated,
			ChangedKeys: []string{"LOG_LEVEL"},
			Timestamp:  time.Now().Add(-2 * time.Minute),
			Source:     "kubernetes",
		}

		correlation := analyzer.correlateChange(change, "incident-002", TimeWindow{
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now(),
		})

		if correlation == nil {
			t.Fatal("Expected correlation, got nil")
		}

		// ConfigMap change should have moderate correlation score
		if correlation.CorrelationScore < 0.5 {
			t.Errorf("ConfigMap change should have correlation score >= 0.5, got %f", correlation.CorrelationScore)
		}

		if correlation.ChangeType != ChangeTypeConfig {
			t.Errorf("Expected change type %s, got %s", ChangeTypeConfig, correlation.ChangeType)
		}
	})

	t.Run("Sensitive Key Change Higher Correlation", func(t *testing.T) {
		analyzer := NewConfigAnalyzer(nil)

		change := ConfigChangeEvent{
			ID:         "configmap-sensitive-001",
			Type:       ConfigTypeConfigMap,
			Namespace:  "production",
			Name:       "database-config",
			Operation:  ConfigOperationUpdated,
			ChangedKeys: []string{"DATABASE_URL", "DB_PASSWORD"},
			Timestamp:  time.Now().Add(-2 * time.Minute),
			Source:     "kubernetes",
		}

		correlation := analyzer.correlateChange(change, "incident-003", TimeWindow{
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now(),
		})

		if correlation == nil {
			t.Fatal("Expected correlation, got nil")
		}

		// Sensitive key change should have high correlation score
		if correlation.CorrelationScore < 0.6 {
			t.Errorf("Sensitive key change should have correlation score >= 0.6, got %f", correlation.CorrelationScore)
		}
	})

	t.Run("Change Too Long Ago No Correlation", func(t *testing.T) {
		analyzer := NewConfigAnalyzer(nil)

		change := ConfigChangeEvent{
			ID:         "old-change-001",
			Type:       ConfigTypeConfigMap,
			Namespace:  "production",
			Name:       "old-config",
			Operation:  ConfigOperationUpdated,
			ChangedKeys: []string{"LOG_LEVEL"},
			Timestamp:  time.Now().Add(-1 * time.Hour),
			Source:     "kubernetes",
		}

		correlation := analyzer.correlateChange(change, "incident-004", TimeWindow{
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now(),
		})

		// Old change should not correlate
		if correlation != nil {
			t.Errorf("Expected no correlation for change 1 hour ago, got score %f", correlation.CorrelationScore)
		}
	})
}

func TestGetConfigSeverity(t *testing.T) {
	t.Run("Secret Change is Critical", func(t *testing.T) {
		change := ConfigChangeEvent{
			Type:       ConfigTypeSecret,
			Namespace:  "production",
			Name:       "api-secret",
			ChangedKeys: []string{"API_KEY"},
		}

		severity := GetConfigSeverity(change)
		if severity != SeverityCritical {
			t.Errorf("Expected severity %s, got %s", SeverityCritical, severity)
		}
	})

	t.Run("Sensitive Key Change is High", func(t *testing.T) {
		change := ConfigChangeEvent{
			Type:       ConfigTypeConfigMap,
			Namespace:  "production",
			Name:       "app-config",
			ChangedKeys: []string{"DATABASE_PASSWORD"},
		}

		severity := GetConfigSeverity(change)
		if severity != SeverityHigh {
			t.Errorf("Expected severity %s, got %s", SeverityHigh, severity)
		}
	})

	t.Run("Regular Config Change is Medium", func(t *testing.T) {
		change := ConfigChangeEvent{
			Type:       ConfigTypeConfigMap,
			Namespace:  "production",
			Name:       "app-config",
			ChangedKeys: []string{"LOG_LEVEL"},
		}

		severity := GetConfigSeverity(change)
		if severity != SeverityMedium {
			t.Errorf("Expected severity %s, got %s", SeverityMedium, severity)
		}
	})

	t.Run("Widespread Change is High", func(t *testing.T) {
		change := ConfigChangeEvent{
			Type:       ConfigTypeConfigMap,
			Namespace:  "production",
			Name:       "global-config",
			ChangedKeys:    []string{"FEATURE_FLAG"},
			AffectedServices: []string{"svc1", "svc2", "svc3", "svc4", "svc5", "svc6"},
		}

		severity := GetConfigSeverity(change)
		if severity != SeverityHigh {
			t.Errorf("Expected severity %s for widespread change, got %s", SeverityHigh, severity)
		}
	})
}

func TestKubernetesCollector(t *testing.T) {
	collector := NewKubernetesCollector(
		WithCluster("test-cluster"),
		WithNamespaces([]string{"production", "staging"}),
	)

	t.Run("Collect Config Changes", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		changes, err := collector.CollectChanges(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to collect changes: %v", err)
		}

		// Should return mock data
		if len(changes) == 0 {
			t.Error("Expected at least one config change")
		}

		for _, c := range changes {
			if c.Source != "kubernetes" {
				t.Errorf("Expected source 'kubernetes', got %s", c.Source)
			}
		}
	})
}

func TestConfigOperation(t *testing.T) {
	t.Run("Operation Values", func(t *testing.T) {
		operations := []ConfigOperation{
			ConfigOperationCreated,
			ConfigOperationUpdated,
			ConfigOperationDeleted,
		}

		for _, op := range operations {
			if op == "" {
				t.Error("Operation should not be empty")
			}
		}
	})
}

func TestConfigType(t *testing.T) {
	t.Run("Type Values", func(t *testing.T) {
		types := []ConfigType{
			ConfigTypeConfigMap,
			ConfigTypeSecret,
			ConfigTypeEnvVar,
			ConfigTypeDeployment,
		}

		for _, ct := range types {
			if ct == "" {
				t.Error("Config type should not be empty")
			}
		}
	})
}
