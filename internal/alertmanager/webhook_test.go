// Package alertmanager provides tests for Alertmanager webhook integration.
package alertmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/llm"
)

// TestWebhookPayloadParsing tests parsing Alertmanager webhook payloads.
func TestWebhookPayloadParsing(t *testing.T) {
	payload := `{
		"version": "4",
		"groupKey": "test-group",
		"truncatedAlerts": false,
		"alerts": [
			{
				"status": "firing",
				"labels": {
					"alertname": "HighErrorRate",
					"severity": "P2",
					"service": "order-service"
				},
				"annotations": {
					"summary": "High error rate detected",
					"description": "Error rate exceeded 5% threshold"
				},
				"startsAt": "2024-01-01T10:00:00Z",
				"endsAt": "0001-01-01T00:00:00Z",
				"generatorURL": "http://prometheus:9090/...",
				"fingerprint": "abc123"
			}
		],
		"groupLabels": {
			"alertname": "HighErrorRate",
			"severity": "P2"
		},
		"commonLabels": {
			"alertname": "HighErrorRate",
			"severity": "P2"
		},
		"commonAnnotations": {},
		"externalURL": "http://alertmanager:9093"
	}`

	var webhookPayload WebhookPayload
	err := json.Unmarshal([]byte(payload), &webhookPayload)
	if err != nil {
		t.Fatalf("Failed to parse payload: %v", err)
	}

	if webhookPayload.getVersion() != "4" {
		t.Errorf("Expected version 4, got %s", webhookPayload.getVersion())
	}

	if len(webhookPayload.Alerts) != 1 {
		t.Errorf("Expected 1 alert, got %d", len(webhookPayload.Alerts))
	}

	alert := webhookPayload.Alerts[0]
	if alert.Status != "firing" {
		t.Errorf("Expected firing status, got %s", alert.Status)
	}

	if alert.Labels["service"] != "order-service" {
		t.Errorf("Expected service order-service, got %s", alert.Labels["service"])
	}
}

// TestIncidentCreation tests incident creation from alerts.
func TestIncidentCreation(t *testing.T) {
	receiver := NewReceiver(
		WithIncidentStore(NewMemoryIncidentStore()),
	)

	alert := Alert{
		Status: "firing",
		Labels: map[string]string{
			"alertname": "DatabaseConnectionExhaustion",
			"severity":  "P1",
			"service":   "order-service",
		},
		Annotations: map[string]string{
			"summary":     "Database connection pool exhausted",
			"description": "Connection pool usage at 100%",
		},
		StartsAt:    time.Now(),
		EndsAt:      time.Time{},
		Fingerprint: "test-fingerprint-123",
	}

	payload := WebhookPayload{
		Alerts:            []Alert{alert},
		CommonLabels:      alert.Labels,
		CommonAnnotations: alert.Annotations,
		ExternalURL:       "http://test",
	}

	incident, err := receiver.createOrUpdateIncident(context.Background(), alert, payload)
	if err != nil {
		t.Fatalf("Failed to create incident: %v", err)
	}

	if incident.Severity != "P1" {
		t.Errorf("Expected severity P1, got %s", incident.Severity)
	}

	if incident.Status != "active" {
		t.Errorf("Expected status active, got %s", incident.Status)
	}

	if len(incident.AffectedServices) != 1 {
		t.Errorf("Expected 1 affected service, got %d", len(incident.AffectedServices))
	}

	if incident.AffectedServices[0] != "order-service" {
		t.Errorf("Expected order-service, got %s", incident.AffectedServices[0])
	}
}

// TestIncidentLifecycle tests lifecycle state transitions.
func TestIncidentLifecycle(t *testing.T) {
	store := NewMemoryIncidentStore()
	incident := &Incident{
		ID:        "INC-001",
		Severity:  "P1",
		Status:    "active",
		StartTime: time.Now(),
	}

	lifecycle := NewIncidentLifecycle(incident, store)

	t.Run("initial_state_is_detected", func(t *testing.T) {
		if lifecycle.CurrentState() != StateDetected {
			t.Errorf("Expected initial state %s, got %s", StateDetected, lifecycle.CurrentState())
		}
	})

	t.Run("valid_transition_to_investigating", func(t *testing.T) {
		err := lifecycle.Transition(context.Background(), StateInvestigating, "Starting RCA", "system")
		if err != nil {
			t.Errorf("Failed to transition: %v", err)
		}

		if lifecycle.CurrentState() != StateInvestigating {
			t.Errorf("Expected state %s, got %s", StateInvestigating, lifecycle.CurrentState())
		}
	})

	t.Run("invalid_transition_from_investigating_to_detected", func(t *testing.T) {
		err := lifecycle.Transition(context.Background(), StateDetected, "Going back", "system")
		if err == nil {
			t.Error("Expected error for invalid transition")
		}
	})

	t.Run("full_lifecycle_flow", func(t *testing.T) {
		// Detected -> Investigating
		_ = lifecycle.Transition(context.Background(), StateInvestigating, "Analysis started", "system")

		// Investigating -> Identified
		_ = lifecycle.Transition(context.Background(), StateIdentified, "Root cause found", "analyst")

		// Identified -> Resolved
		_ = lifecycle.Transition(context.Background(), StateResolved, "Fix deployed", "ops")

		// Resolved -> Closed
		err := lifecycle.Transition(context.Background(), StateClosed, "Archiving", "system")
		if err != nil {
			t.Errorf("Failed to transition to closed: %v", err)
		}

		// Verify history
		history := lifecycle.History()
		if len(history) != 5 { // Initial + 4 transitions
			t.Errorf("Expected 5 history entries, got %d", len(history))
		}
	})
}

// TestIncidentTimer tests incident timing tracking.
func TestIncidentTimer(t *testing.T) {
	timer := NewIncidentTimer()

	t.Run("tracks_state_durations", func(t *testing.T) {
		timer.EnterState(StateDetected)
		time.Sleep(50 * time.Millisecond)

		timer.EnterState(StateInvestigating)
		detectedDuration := timer.GetDuration(StateDetected)

		if detectedDuration < 50*time.Millisecond {
			t.Errorf("Expected detected duration > 50ms, got %v", detectedDuration)
		}

		time.Sleep(30 * time.Millisecond)
		investigatingDuration := timer.GetDuration(StateInvestigating)

		if investigatingDuration < 30*time.Millisecond {
			t.Errorf("Expected investigating duration > 30ms, got %v", investigatingDuration)
		}
	})

	t.Run("calculates_total_duration", func(t *testing.T) {
		timer := NewIncidentTimer()
		timer.EnterState(StateDetected)
		time.Sleep(20 * time.Millisecond)

		timer.EnterState(StateInvestigating)
		time.Sleep(20 * time.Millisecond)

		total := timer.GetTotalDuration()
		if total < 40*time.Millisecond {
			t.Errorf("Expected total duration > 40ms, got %v", total)
		}
	})
}

// TestMemoryIncidentStore tests the in-memory incident store.
func TestMemoryIncidentStore(t *testing.T) {
	store := NewMemoryIncidentStore()

	t.Run("stores_and_retrieves_incident", func(t *testing.T) {
		incident := Incident{
			ID:          "INC-001",
			Severity:    "P1",
			Title:       "Test Incident",
			Status:      "active",
			StartTime:   time.Now(),
			AffectedServices: []string{"order-service"},
		}

		ctx := context.Background()
		err := store.Store(ctx, incident)
		if err != nil {
			t.Fatalf("Failed to store incident: %v", err)
		}

		retrieved, err := store.Get(ctx, "INC-001")
		if err != nil {
			t.Fatalf("Failed to retrieve incident: %v", err)
		}

		if retrieved.ID != incident.ID {
			t.Errorf("Expected ID %s, got %s", incident.ID, retrieved.ID)
		}
	})

	t.Run("lists_with_filters", func(t *testing.T) {
		// Create multiple incidents
		incidents := []Incident{
			{ID: "INC-001", Severity: "P1", Status: "active", StartTime: time.Now(), AffectedServices: []string{"order-service"}},
			{ID: "INC-002", Severity: "P2", Status: "active", StartTime: time.Now(), AffectedServices: []string{"payment-service"}},
			{ID: "INC-003", Severity: "P1", Status: "resolved", StartTime: time.Now().Add(-1 * time.Hour), AffectedServices: []string{"order-service"}},
		}

		ctx := context.Background()
		for _, inc := range incidents {
			_ = store.Store(ctx, inc)
		}

		// Filter by P1 severity
		filter := IncidentFilter{Severity: "P1"}
		results, err := store.List(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to list: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Expected 2 P1 incidents, got %d", len(results))
		}

		// Filter by order-service
		filter = IncidentFilter{Service: "order-service"}
		results, err = store.List(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to list: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Expected 2 order-service incidents, got %d", len(results))
		}
	})
}

// TestWebhookHandler tests the webhook HTTP handler.
func TestWebhookHandler(t *testing.T) {
	receiver := NewReceiver(
		WithAddress(":0"), // Use random port
		WithIncidentStore(NewMemoryIncidentStore()),
	)

	server := httptest.NewServer(http.HandlerFunc(receiver.handleWebhook))
	defer server.Close()

	payload := WebhookPayload{
		Version: "4",
		GroupKey: "test-group",
		Alerts: []Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "TestAlert",
					"severity":  "P2",
				},
				StartsAt:    time.Now(),
				Fingerprint: "test-fp",
			},
		},
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(server.URL+"/webhooks/alertmanager", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// Helper functions for accessing private fields in tests

func (p WebhookPayload) getVersion() string {
	return p.Version
}

func (r *Receiver) createOrUpdateIncident(ctx context.Context, alert Alert, payload WebhookPayload) (Incident, error) {
	return r.createOrUpdateIncident(ctx, alert, payload)
}

func (r *Receiver) processAlert(ctx context.Context, alert Alert, payload WebhookPayload) error {
	return r.processAlert(ctx, alert, payload)
}

func (r *Receiver) collectEvidence(ctx context.Context, incident Incident) ([]Evidence, error) {
	return r.collectEvidence(ctx, incident)
}
