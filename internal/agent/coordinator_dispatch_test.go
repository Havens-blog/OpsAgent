package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
)

// TestCoordinatorDelegateDispatchesToSpecialist verifies that DelegateTask
// calls the concrete MockAgent.Execute rather than BaseAgent.Execute.
func TestCoordinatorDelegateDispatchesToSpecialist(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	coord, err := NewCoordinatorAgent(settings)
	if err != nil {
		t.Fatalf("NewCoordinatorAgent failed: %v", err)
	}
	defer coord.Shutdown(context.Background())

	mockAgent, err := NewMockAgent(settings)
	if err != nil {
		t.Fatalf("NewMockAgent failed: %v", err)
	}

	if err := coord.RegisterAgent("mock", mockAgent); err != nil {
		t.Fatalf("RegisterAgent failed: %v", err)
	}

	ctx := context.Background()
	task := &Task{
		ID:          "dispatch-test",
		Description: "verify specialist dispatch",
		Params:      map[string]interface{}{},
		CreatedAt:   time.Now(),
		Status:      "pending",
	}

	submission, err := coord.DelegateTask(ctx, "mock", task)
	if err != nil {
		t.Fatalf("DelegateTask failed: %v", err)
	}

	if mockAgent.GetExecuteCount() != 1 {
		t.Fatalf("expected concrete MockAgent.Execute to run once, got %d", mockAgent.GetExecuteCount())
	}
	if submission.Result == nil {
		t.Fatal("expected non-nil submission result")
	}
	if got, ok := submission.Result.Data["exec_count"]; !ok || got != 1 {
		t.Fatalf("expected exec_count == 1, got %v (present=%v)", got, ok)
	}
	if !strings.Contains(submission.Result.Response, "Mock execution #1") {
		t.Fatalf("expected response to contain %q, got %q", "Mock execution #1", submission.Result.Response)
	}
}
