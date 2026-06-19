// Package alertmanager provides tests for Seed Call functionality.
package alertmanager

import (
	"context"
	"testing"
	"time"
)

// TestSeedCallExecution tests the Seed Call execution.
func TestSeedCallExecution(t *testing.T) {
	config := &SeedCallConfig{
		LookbackDuration:    15 * time.Minute,
		CollectionDuration:  30 * time.Minute,
		QueryTimeout:        5 * time.Second,
		ParallelCollection:  true,
	}

	collectors := []EvidenceCollector{
		NewMockLogCollector("sls-collector", 100*time.Millisecond, false, 10),
		NewMockLogCollector("elasticsearch-collector", 50*time.Millisecond, false, 5),
		NewMockLogCollector("metrics-collector", 75*time.Millisecond, false, 3),
	}

	seedCall := NewSeedCall(config, collectors)

	incident := Incident{
		ID:          "INC-001",
		Severity:    "P1",
		Title:       "Test Incident",
		Status:      "active",
		StartTime:   time.Now(),
		EndTime:     time.Now(),
		AffectedServices: []string{"order-service"},
	}

	result, err := seedCall.Execute(context.Background(), incident)
	if err != nil {
		t.Fatalf("Seed Call failed: %v", err)
	}

	// Verify results
	if result.IncidentID != incident.ID {
		t.Errorf("Expected incident ID %s, got %s", incident.ID, result.IncidentID)
	}

	if result.TotalEvidence != 18 { // 10 + 5 + 3
		t.Errorf("Expected 18 evidence items, got %d", result.TotalEvidence)
	}

	if len(result.Results) != 3 {
		t.Errorf("Expected 3 collector results, got %d", len(result.Results))
	}

	if len(result.Errors) > 0 {
		t.Errorf("Expected no errors, got %d", len(result.Errors))
	}

	t.Logf("Seed Call summary: %s", result.Summary())
}

// TestSeedCallWithFailedCollector tests Seed Call with a failing collector.
func TestSeedCallWithFailedCollector(t *testing.T) {
	config := DefaultSeedCallConfig()

	collectors := []EvidenceCollector{
		NewMockLogCollector("working-collector", 50*time.Millisecond, false, 5),
		NewMockLogCollector("failing-collector", 50*time.Millisecond, true, 0),
	}

	seedCall := NewSeedCall(config, collectors)

	incident := Incident{
		ID:        "INC-001",
		StartTime: time.Now(),
		AffectedServices: []string{"test-service"},
	}

	result, err := seedCall.Execute(context.Background(), incident)
	if err != nil {
		t.Fatalf("Seed Call should succeed despite collector failure: %v", err)
	}

	// Should have partial results
	if result.TotalEvidence != 5 {
		t.Errorf("Expected 5 evidence items, got %d", result.TotalEvidence)
	}

	// Should have recorded the error
	if len(result.Errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(result.Errors))
	}

	failedCollectors := result.GetFailedCollectors()
	if len(failedCollectors) != 1 || failedCollectors[0] != "failing-collector" {
		t.Errorf("Expected failing-collector in failed list, got %v", failedCollectors)
	}
}

// TestSeedCallSequentialCollection tests sequential evidence collection.
func TestSeedCallSequentialCollection(t *testing.T) {
	config := &SeedCallConfig{
		ParallelCollection: false,
		QueryTimeout:        5 * time.Second,
	}

	collectors := []EvidenceCollector{
		NewMockLogCollector("collector-1", 50*time.Millisecond, false, 5),
		NewMockLogCollector("collector-2", 50*time.Millisecond, false, 5),
	}

	seedCall := NewSeedCall(config, collectors)

	incident := Incident{
		ID:        "INC-001",
		StartTime: time.Now(),
		AffectedServices: []string{"test-service"},
	}

	start := time.Now()
	result, err := seedCall.Execute(context.Background(), incident)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Seed Call failed: %v", err)
	}

	// Sequential collection should take roughly sum of delays
	expectedMinDuration := 100 * time.Millisecond
	if duration < expectedMinDuration {
		t.Errorf("Expected duration > %v, got %v", expectedMinDuration, duration)
	}

	if result.TotalEvidence != 10 {
		t.Errorf("Expected 10 evidence items, got %d", result.TotalEvidence)
	}

	t.Logf("Sequential collection completed in %v", duration)
}

// TestSeedCallCancellation tests Seed Call cancellation.
func TestSeedCallCancellation(t *testing.T) {
	config := DefaultSeedCallConfig()

	// Create a slow collector
	collectors := []EvidenceCollector{
		NewMockLogCollector("slow-collector", 5*time.Second, false, 100),
	}

	seedCall := NewSeedCall(config, collectors)

	incident := Incident{
		ID:        "INC-001",
		StartTime: time.Now(),
		AffectedServices: []string{"test-service"},
	}

	// Cancel context after short delay
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := seedCall.Execute(ctx, incident)

	// Should fail due to timeout
	if err == nil {
		t.Error("Expected error due to cancellation")
	}

	if result != nil && result.TotalEvidence > 0 {
		t.Error("Expected no evidence due to cancellation")
	}
}

// TestEvidenceByType tests grouping evidence by type.
func TestEvidenceByType(t *testing.T) {
	config := DefaultSeedCallConfig()

	collectors := []EvidenceCollector{
		NewMockLogCollector("log-collector", 50*time.Millisecond, false, 5),
		NewMockLogCollector("metric-collector", 50*time.Millisecond, false, 3, "metrics"),
	}

	seedCall := NewSeedCall(config, collectors)

	incident := Incident{
		ID:        "INC-001",
		StartTime: time.Now(),
		AffectedServices: []string{"test-service"},
	}

	result, err := seedCall.Execute(context.Background(), incident)
	if err != nil {
		t.Fatalf("Seed Call failed: %v", err)
	}

	evidenceByType := result.GetEvidenceByType()

	// Should have both logs and metrics
	if len(evidenceByType) != 2 {
		t.Errorf("Expected 2 evidence types, got %d", len(evidenceByType))
	}

	logEvidence, ok := evidenceByType["logs"]
	if !ok {
		t.Error("Expected logs evidence type")
	}

	if len(logEvidence) != 5 {
		t.Errorf("Expected 5 log items, got %d", len(logEvidence))
	}

	metricEvidence, ok := evidenceByType["metrics"]
	if !ok {
		t.Error("Expected metrics evidence type")
	}

	if len(metricEvidence) != 3 {
		t.Errorf("Expected 3 metric items, got %d", len(metricEvidence))
	}
}

// TestSeedCallTimeWindow tests time window calculation.
func TestSeedCallTimeWindow(t *testing.T) {
	config := &SeedCallConfig{
		LookbackDuration:    15 * time.Minute,
		CollectionDuration:  30 * time.Minute,
	}

	seedCall := NewSeedCall(config, nil)

	incident := Incident{
		ID:        "INC-001",
		StartTime: time.Now(),
		AffectedServices: []string{"test-service"},
	}

	// Create a mock collector to verify time window
	timeWindowCollector := &TimeWindowCollector{}
	seedCall = NewSeedCall(config, []EvidenceCollector{timeWindowCollector})

	_, err := seedCall.Execute(context.Background(), incident)
	if err != nil {
		t.Fatalf("Seed Call failed: %v", err)
	}

	// Verify time window
	expectedStart := incident.StartTime.Add(-15 * time.Minute)
	if timeWindowCollector.receivedStartTime.Sub(expectedStart) > time.Second {
		t.Errorf("Expected start time around %v, got %v",
			expectedStart, timeWindowCollector.receivedStartTime)
	}

	maxDuration := 30 * time.Minute
	windowDuration := timeWindowCollector.receivedEndTime.Sub(timeWindowCollector.receivedStartTime)
	if windowDuration > maxDuration+time.Second {
		t.Errorf("Expected window duration < %v, got %v",
			maxDuration, windowDuration)
	}

	t.Logf("Time window: %v to %v (duration: %v)",
		timeWindowCollector.receivedStartTime,
		timeWindowCollector.receivedEndTime,
		windowDuration)
}

// TimeWindowCollector is a mock collector that records the received time window.
type TimeWindowCollector struct {
	receivedStartTime time.Time
	receivedEndTime   time.Time
}

func (t *TimeWindowCollector) Collect(ctx context.Context, req CollectionRequest) (*CollectionResult, error) {
	t.receivedStartTime = req.StartTime
	t.receivedEndTime = req.EndTime

	return &CollectionResult{
		Evidence: []Evidence{
			{
				Type:        "time_window",
				Description: "Time window verification",
			},
		},
	}, nil
}

func (t *TimeWindowCollector) Name() string {
	return "time-window-collector"
}

func (t *TimeWindowCollector) Type() string {
	return "metadata"
}
