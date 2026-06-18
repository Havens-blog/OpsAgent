// Package alertmanager provides Alertmanager webhook integration for OpsAgent.
// This module receives alerts from Prometheus Alertmanager and triggers RCA analysis.
package alertmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/llm"
	"github.com/Havens-blog/OpsAgent/internal/observability"
)

// WebhookPayload represents an Alertmanager webhook notification.
type WebhookPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   bool              `json:"truncatedAlerts"`
	Alerts            []Alert          `json:"alerts"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
}

// Alert represents a single alert from Alertmanager.
type Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// Receiver handles Alertmanager webhook notifications.
type Receiver struct {
	address      string
	llmClient    *llm.ClaudeClient
	timeout      time.Duration
	incidentStore IncidentStore
	logger       *observability.Logger
}

// ReceiverOption configures a Receiver.
type ReceiverOption func(*Receiver)

// WithAddress sets the listener address.
func WithAddress(addr string) ReceiverOption {
	return func(r *Receiver) {
		r.address = addr
	}
}

// WithTimeout sets the request timeout.
func WithTimeout(timeout time.Duration) ReceiverOption {
	return func(r *Receiver) {
		r.timeout = timeout
	}
}

// WithLLMClient sets the LLM client for analysis.
func WithLLMClient(client *llm.ClaudeClient) ReceiverOption {
	return func(r *Receiver) {
		r.llmClient = client
	}
}

// WithIncidentStore sets the incident store.
func WithIncidentStore(store IncidentStore) ReceiverOption {
	return func(r *Receiver) {
		r.incidentStore = store
	}
}

// NewReceiver creates a new Alertmanager webhook receiver.
func NewReceiver(opts ...ReceiverOption) *Receiver {
	logger, _ := observability.NewLogger("alertmanager", "info", observability.FormatJSON)
	r := &Receiver{
		address:   ":8080",
		timeout:   30 * time.Second,
		llmClient: nil,
		logger:    logger,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Start starts the webhook receiver server.
func (r *Receiver) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhooks/alertmanager", r.handleWebhook)

	server := &http.Server{
		Addr:         r.address,
		Handler:      mux,
		ReadTimeout:  r.timeout,
		WriteTimeout: r.timeout,
	}

	r.logger.Info("Starting Alertmanager webhook receiver", observability.String("address", r.address))

	errChan := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("server error: %w", err)
		}
		close(errChan)
	}()

	select {
	case <-ctx.Done():
		r.logger.Info("Shutting down webhook receiver")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown error: %w", err)
		}
		return nil
	case err := <-errChan:
		return err
	}
}

// handleWebhook processes incoming webhook notifications.
func (r *Receiver) handleWebhook(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read payload
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Parse payload
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Failed to parse payload", http.StatusBadRequest)
		return
	}

	r.logger.Info("Received Alertmanager webhook",
		observability.Int("alert_count", len(payload.Alerts)),
		observability.String("group_key", payload.GroupKey),
		observability.String("status", payload.getStatus()))

	// Process alerts
	for _, alert := range payload.Alerts {
		if err := r.processAlert(req.Context(), alert, payload); err != nil {
			r.logger.Error("Failed to process alert", observability.Err(err), observability.String("fingerprint", alert.Fingerprint))
			// Continue processing other alerts
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "acknowledged"}`))
}

// processAlert processes a single alert.
func (r *Receiver) processAlert(ctx context.Context, alert Alert, payload WebhookPayload) error {
	// Check if this is a new or updated alert
	incident, err := r.createOrUpdateIncident(ctx, alert, payload)
	if err != nil {
		return fmt.Errorf("failed to create/update incident: %w", err)
	}

	// If alert is firing, trigger analysis
	if alert.Status == "firing" {
		if r.llmClient != nil {
			go r.analyzeIncident(context.Background(), incident)
		}
	}

	return nil
}

// createOrUpdateIncident creates or updates an incident from an alert.
func (r *Receiver) createOrUpdateIncident(ctx context.Context, alert Alert, payload WebhookPayload) (Incident, error) {
	incident := Incident{
		ID:          generateIncidentID(alert),
		Severity:    extractSeverity(alert, payload),
		Title:       extractTitle(alert, payload),
		Description: extractDescription(alert, payload),
		StartTime:   alert.StartsAt,
		EndTime:     alert.EndsAt,
		Status:      mapStatus(alert.Status),
		Labels:      alert.Labels,
		Annotations: alert.Annotations,
		AffectedServices: extractServices(alert, payload),
		Alerts:      []Alert{alert},
		GroupKey:    payload.GroupKey,
		ExternalURL: payload.ExternalURL,
	}

	// Store incident
	if r.incidentStore != nil {
		if err := r.incidentStore.Store(ctx, incident); err != nil {
			return Incident{}, fmt.Errorf("failed to store incident: %w", err)
		}
	}

	return incident, nil
}

// analyzeIncident performs RCA analysis on an incident.
func (r *Receiver) analyzeIncident(ctx context.Context, incident Incident) {
	r.logger.Info("Starting incident analysis", observability.String("incident_id", incident.ID))

	// Seed call: collect initial evidence
	evidence, err := r.collectEvidence(ctx, incident)
	if err != nil {
		r.logger.Error("Failed to collect evidence", observability.Err(err), observability.String("incident_id", incident.ID))
		return
	}

	// Perform RCA analysis
	// This would integrate with the RCA engine
	r.logger.Info("Evidence collected",
		observability.String("incident_id", incident.ID),
		observability.Int("evidence_count", len(evidence)))

	// Update incident with findings
	if r.incidentStore != nil {
		incident.Evidence = evidence
		_ = r.incidentStore.Update(ctx, incident)
	}
}

// collectEvidence performs initial evidence collection for an incident.
func (r *Receiver) collectEvidence(ctx context.Context, incident Incident) ([]Evidence, error) {
	// Seed call: collect logs, metrics, and traces
	var evidence []Evidence

	// Time window for evidence collection
	startTime := incident.StartTime.Add(-5 * time.Minute)
	endTime := incident.EndTime
	if endTime.IsZero() {
		endTime = time.Now()
	}

	// Collect evidence for each affected service
	for _, service := range incident.AffectedServices {
		// Logs evidence
		evidence = append(evidence, Evidence{
			Type:        "logs",
			Source:      service,
			StartTime:   startTime,
			EndTime:     endTime,
			Description: fmt.Sprintf("Logs for %s during incident", service),
		})

		// Metrics evidence
		evidence = append(evidence, Evidence{
			Type:        "metrics",
			Source:      service,
			StartTime:   startTime,
			EndTime:     endTime,
			Description: fmt.Sprintf("Metrics for %s during incident", service),
		})

		// Change events evidence
		evidence = append(evidence, Evidence{
			Type:        "changes",
			Source:      "kubernetes",
			StartTime:   startTime.Add(-30 * time.Minute),
			EndTime:     endTime,
			Description: fmt.Sprintf("Recent changes affecting %s", service),
		})
	}

	return evidence, nil
}

// getStatus returns the overall status of the payload.
func (p *WebhookPayload) getStatus() string {
	if len(p.Alerts) == 0 {
		return "unknown"
	}

	// If any alert is firing, the status is firing
	for _, alert := range p.Alerts {
		if alert.Status == "firing" {
			return "firing"
		}
	}
	return "resolved"
}

// Incident represents an incident created from alerts.
type Incident struct {
	ID              string              `json:"id"`
	Severity        string              `json:"severity"`
	Title           string              `json:"title"`
	Description     string              `json:"description"`
	StartTime       time.Time           `json:"start_time"`
	EndTime         time.Time           `json:"end_time"`
	Status          string              `json:"status"`
	Labels          map[string]string   `json:"labels"`
	Annotations     map[string]string   `json:"annotations"`
	AffectedServices []string           `json:"affected_services"`
	Alerts          []Alert             `json:"alerts"`
	GroupKey        string              `json:"group_key"`
	ExternalURL     string              `json:"external_url"`
	Evidence        []Evidence          `json:"evidence,omitempty"`
	RootCause       string              `json:"root_cause,omitempty"`
	Confidence      float64             `json:"confidence,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

// Evidence represents collected evidence for an incident.
type Evidence struct {
	Type        string    `json:"type"`
	Source      string    `json:"source"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Description string    `json:"description"`
	Data        any       `json:"data,omitempty"`
}

// IncidentStore defines the interface for storing incidents.
type IncidentStore interface {
	Store(ctx context.Context, incident Incident) error
	Get(ctx context.Context, id string) (Incident, error)
	Update(ctx context.Context, incident Incident) error
	List(ctx context.Context, filter IncidentFilter) ([]Incident, error)
}

// IncidentFilter defines filters for listing incidents.
type IncidentFilter struct {
	Severity  string
	Status    string
	Service   string
	StartTime time.Time
	EndTime   time.Time
}

// generateIncidentID generates a unique incident ID from an alert.
func generateIncidentID(alert Alert) string {
	timestamp := alert.StartsAt.Format("20060102150405")
	return fmt.Sprintf("INC-%s-%s", timestamp, alert.Fingerprint[:8])
}

// extractSeverity extracts severity from an alert.
func extractSeverity(alert Alert, payload WebhookPayload) string {
	// Try common severity labels
	if severity, ok := alert.Labels["severity"]; ok {
		return severity
	}
	if severity, ok := payload.CommonLabels["severity"]; ok {
		return severity
	}

	// Default to P3
	return "P3"
}

// extractTitle extracts a title from an alert.
func extractTitle(alert Alert, payload WebhookPayload) string {
	// Try annotations first
	if title, ok := alert.Annotations["title"]; ok {
		return title
	}
	if title, ok := alert.Annotations["summary"]; ok {
		return title
	}
	if title, ok := payload.CommonAnnotations["summary"]; ok {
		return title
	}

	// Fall back to alert name
	if name, ok := alert.Labels["alertname"]; ok {
		return name
	}

	return "Untitled Alert"
}

// extractDescription extracts a description from an alert.
func extractDescription(alert Alert, payload WebhookPayload) string {
	if desc, ok := alert.Annotations["description"]; ok {
		return desc
	}
	if desc, ok := payload.CommonAnnotations["description"]; ok {
		return desc
	}

	return ""
}

// extractServices extracts affected services from an alert.
func extractServices(alert Alert, payload WebhookPayload) []string {
	var services []string

	// Try common service labels
	serviceLabels := []string{"service", "job", "app", "application"}
	for _, label := range serviceLabels {
		if service, ok := alert.Labels[label]; ok {
			services = append(services, service)
		}
		if service, ok := payload.CommonLabels[label]; ok {
			services = append(services, service)
		}
	}

	// Remove duplicates
	seen := make(map[string]bool)
	var unique []string
	for _, s := range services {
		if !seen[s] {
			seen[s] = true
			unique = append(unique, s)
		}
	}

	if len(unique) == 0 {
		return []string{"unknown"}
	}

	return unique
}

// mapStatus maps Alertmanager status to incident status.
func mapStatus(status string) string {
	switch status {
	case "firing":
		return "active"
	case "resolved":
		return "resolved"
	default:
		return "unknown"
	}
}
