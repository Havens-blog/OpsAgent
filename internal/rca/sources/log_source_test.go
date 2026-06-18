// Package sources provides tests for log source interfaces.
package sources

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// MockLogSourceForTest is a test implementation of LogSource.
type MockLogSourceForTest struct{}

func (m *MockLogSourceForTest) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	return []models.LogEntry{
		{Timestamp: start, Service: "test-service", Level: "error", Message: "Test error"},
	}, nil
}

func (m *MockLogSourceForTest) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	return []models.LogEntry{
		{Timestamp: start, Service: service, Level: "info", Message: "Test log"},
	}, nil
}

func (m *MockLogSourceForTest) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	return 42, nil
}

// TestLogSourceInterface tests that implementations satisfy the interface.
func TestLogSourceInterface(t *testing.T) {
	ctx := context.Background()
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	mock := &MockLogSourceForTest{}

	// Verify interface compliance by calling all methods
	entries, err := mock.QueryErrors(ctx, start, end)
	if err != nil {
		t.Fatalf("QueryErrors failed: %v", err)
	}
	if len(entries) == 0 {
		t.Error("Expected at least one error entry")
	}

	entries, err = mock.QueryLogsByService(ctx, "test-service", start, end)
	if err != nil {
		t.Fatalf("QueryLogsByService failed: %v", err)
	}
	if len(entries) == 0 {
		t.Error("Expected at least one log entry")
	}

	count, err := mock.GetLogCount(ctx, "test-service", start, end)
	if err != nil {
		t.Fatalf("GetLogCount failed: %v", err)
	}
	if count != 42 {
		t.Errorf("Expected count 42, got %d", count)
	}
}
