// Package change provides change detection and correlation for OpsAgent.
// This module tracks CI/CD deployments, configuration changes, and correlates
// them with incident time windows.
package change

import (
	"context"
	"fmt"
	"time"
)

// ChangeType represents the type of change event.
type ChangeType string

const (
	// ChangeTypeDeployment represents a deployment change
	ChangeTypeDeployment ChangeType = "deployment"
	// ChangeTypeConfig represents a configuration change
	ChangeTypeConfig ChangeType = "config"
	// ChangeTypeSecret represents a secret change
	ChangeTypeSecret ChangeType = "secret"
	// ChangeTypeInfrastructure represents an infrastructure change
	ChangeTypeInfrastructure ChangeType = "infrastructure"
	// ChangeTypeCertificate represents a certificate change
	ChangeTypeCertificate ChangeType = "certificate"
)

// ChangeSeverity represents the severity of a change.
type ChangeSeverity string

const (
	SeverityLow    ChangeSeverity = "low"
	SeverityMedium ChangeSeverity = "medium"
	SeverityHigh   ChangeSeverity = "high"
	SeverityCritical ChangeSeverity = "critical"
)

// DeploymentEvent represents a deployment event from CI/CD systems.
type DeploymentEvent struct {
	// Unique identifier for this deployment
	ID string `json:"id"`

	// Type of CI/CD system
	CICDSystem CICDSystem `json:"cicd_system"`

	// Project/repository information
	Project   string `json:"project"`
	Repository string `json:"repository"`

	// Deployment information
	Environment string `json:"environment"` // production, staging, etc.
	Service     string `json:"service"`

	// Version information
	CommitSHA     string `json:"commit_sha"`
	CommitMessage string `json:"commit_message"`
	CommitAuthor  string `json:"commit_author"`
	Tag           string `json:"tag,omitempty"`
	Branch        string `json:"branch"`

	// Pipeline/Job information
	PipelineID   string `json:"pipeline_id"`
	JobID        string `json:"job_id"`
	StageName    string `json:"stage_name,omitempty"`

	// Timing
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Duration    time.Duration `json:"duration"`

	// Status
	Status      DeploymentStatus `json:"status"`
	Rollback   bool             `json:"rollback"`
	RollbackOf string           `json:"rollback_of,omitempty"`

	// Additional metadata
	Changes     []DeploymentChange `json:"changes,omitempty"`
	Artifacts   []Artifact         `json:"artifacts,omitempty"`
	Annotations map[string]string  `json:"annotations,omitempty"`

	// Correlation
	IncidentIDs []string `json:"incident_ids,omitempty"` // Correlated incidents
}

// CICDSystem represents the type of CI/CD system.
type CICDSystem string

const (
	CICDSystemGitLabCI   CICDSystem = "gitlab_ci"
	CICDSystemGitHubActions CICDSystem = "github_actions"
	CICDSystemJenkins    CICDSystem = "jenkins"
	CICDSystemArgoCD     CICDSystem = "argocd"
	CICDSystemFluxCD     CICDSystem = "fluxcd"
	CICDSystemCustom     CICDSystem = "custom"
)

// DeploymentStatus represents the status of a deployment.
type DeploymentStatus string

const (
	DeploymentStatusSuccess    DeploymentStatus = "success"
	DeploymentStatusFailed     DeploymentStatus = "failed"
	DeploymentStatusRunning    DeploymentStatus = "running"
	DeploymentStatusPending    DeploymentStatus = "pending"
	DeploymentStatusRolledBack DeploymentStatus = "rolled_back"
)

// DeploymentChange represents a single change in a deployment.
type DeploymentChange struct {
	Type    string `json:"type"`    // added, modified, deleted
	Path    string `json:"path"`
	OldSHA  string `json:"old_sha,omitempty"`
	NewSHA  string `json:"new_sha,omitempty"`
	Additions int `json:"additions,omitempty"`
	Deletions  int `json:"deletions,omitempty"`
}

// Artifact represents a deployment artifact.
type Artifact struct {
	Name     string `json:"name"`
	Type     string `json:"type"`     // docker, jar, zip, etc.
	Digest   string `json:"digest,omitempty"`
	Registry string `json:"registry,omitempty"`
	Image    string `json:"image,omitempty"`
}

// TimeWindow represents a time window for change correlation.
type TimeWindow struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// ChangeCorrelation represents the correlation between a change and an incident.
type ChangeCorrelation struct {
	ChangeID        string    `json:"change_id"`
	ChangeType      ChangeType `json:"change_type"`
	IncidentID      string    `json:"incident_id"`
	CorrelationScore float64 `json:"correlation_score"`
	Reason          string    `json:"reason"`
	TimeDelta       time.Duration `json:"time_delta"` // Time between change and incident
}

// ChangeStore defines the interface for storing and querying change events.
type ChangeStore interface {
	// StoreDeployment stores a deployment event
	StoreDeployment(ctx context.Context, deployment DeploymentEvent) error

	// GetDeployment retrieves a deployment event by ID
	GetDeployment(ctx context.Context, id string) (DeploymentEvent, error)

	// ListDeployments lists deployment events matching filters
	ListDeployments(ctx context.Context, filter DeploymentFilter) ([]DeploymentEvent, error)

	// StoreCorrelation stores a change-incident correlation
	StoreCorrelation(ctx context.Context, correlation ChangeCorrelation) error

	// GetCorrelations retrieves correlations for an incident
	GetCorrelations(ctx context.Context, incidentID string) ([]ChangeCorrelation, error)
}

// DeploymentFilter defines filters for listing deployments.
type DeploymentFilter struct {
	Project     string
	Repository   string
	Service     string
	Environment string
	Status      DeploymentStatus
	StartTime   *time.Time
	EndTime     *time.Time
	Limit       int
	Offset      int
}

// CICDCollector defines the interface for collecting CI/CD events.
type CICDCollector interface {
	// CollectDeployments collects deployment events within a time window
	CollectDeployments(ctx context.Context, window TimeWindow) ([]DeploymentEvent, error)

	// GetLatestDeployment returns the most recent deployment for a service
	GetLatestDeployment(ctx context.Context, service, environment string) (DeploymentEvent, error)

	// Health checks if the collector can reach the CI/CD system
	Health(ctx context.Context) error
}

// ChangeAnalyzer analyzes changes and correlates them with incidents.
type ChangeAnalyzer struct {
	store     ChangeStore
	collectors map[CICDSystem]CICDCollector
}

// NewChangeAnalyzer creates a new ChangeAnalyzer.
func NewChangeAnalyzer(store ChangeStore) *ChangeAnalyzer {
	return &ChangeAnalyzer{
		store:     store,
		collectors: make(map[CICDSystem]CICDCollector),
	}
}

// RegisterCollector registers a CI/CD collector.
func (a *ChangeAnalyzer) RegisterCollector(system CICDSystem, collector CICDCollector) {
	a.collectors[system] = collector
}

// CollectRecentChanges collects all changes within a time window.
func (a *ChangeAnalyzer) CollectRecentChanges(ctx context.Context, window TimeWindow) ([]DeploymentEvent, error) {
	var allDeployments []DeploymentEvent

	for system, collector := range a.collectors {
		deployments, err := collector.CollectDeployments(ctx, window)
		if err != nil {
			// Log error but continue with other collectors
			fmt.Printf("Failed to collect from %s: %v\n", system, err)
			continue
		}

		// Store deployments
		for _, deployment := range deployments {
			if err := a.store.StoreDeployment(ctx, deployment); err != nil {
				fmt.Printf("Failed to store deployment %s: %v\n", deployment.ID, err)
			}
		}

		allDeployments = append(allDeployments, deployments...)
	}

	return allDeployments, nil
}

// CorrelateWithIncident correlates changes with an incident time window.
func (a *ChangeAnalyzer) CorrelateWithIncident(
	ctx context.Context,
	incidentID string,
	incidentWindow TimeWindow,
) ([]ChangeCorrelation, error) {
	// Query deployments in the time window
	// Look back 30 minutes before incident start
	searchWindow := TimeWindow{
		StartTime: incidentWindow.StartTime.Add(-30 * time.Minute),
		EndTime:   incidentWindow.EndTime,
	}

	filter := DeploymentFilter{
		StartTime: &searchWindow.StartTime,
		EndTime:   &searchWindow.EndTime,
		Limit:     100,
	}

	deployments, err := a.store.ListDeployments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	var correlations []ChangeCorrelation

	for _, deployment := range deployments {
		correlation := a.correlateDeployment(deployment, incidentID, incidentWindow)
		if correlation != nil {
			correlations = append(correlations, *correlation)

			// Store correlation
			if err := a.store.StoreCorrelation(ctx, *correlation); err != nil {
				fmt.Printf("Failed to store correlation: %v\n", err)
			}
		}
	}

	return correlations, nil
}

// correlateDeployment correlates a single deployment with an incident.
func (a *ChangeAnalyzer) correlateDeployment(
	deployment DeploymentEvent,
	incidentID string,
	incidentWindow TimeWindow,
) *ChangeCorrelation {
	// Calculate time delta
	var timeDelta time.Duration
	if deployment.StartedAt.Before(incidentWindow.StartTime) {
		timeDelta = incidentWindow.StartTime.Sub(deployment.StartedAt)
	} else {
		timeDelta = 0
	}

	// Calculate correlation score based on:
	// 1. Time proximity (0-30 minutes = higher score)
	// 2. Deployment status (failed = higher score)
	// 3. Environment match
	// 4. Service match

	score := 0.0
	reasons := []string{}

	// Time proximity scoring
	if timeDelta < 5*time.Minute {
		score += 0.5
		reasons = append(reasons, "deployment within 5 minutes")
	} else if timeDelta < 15*time.Minute {
		score += 0.3
		reasons = append(reasons, "deployment within 15 minutes")
	} else if timeDelta < 30*time.Minute {
		score += 0.1
		reasons = append(reasons, "deployment within 30 minutes")
	}

	// Status scoring
	if deployment.Status == DeploymentStatusFailed {
		score += 0.3
		reasons = append(reasons, "deployment failed")
	} else if deployment.Status == DeploymentStatusRolledBack {
		score += 0.4
		reasons = append(reasons, "deployment was rolled back")
	}

	// Build reason string
	reason := fmt.Sprintf("Deployment %s", deployment.ID)
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
		ChangeID:         deployment.ID,
		ChangeType:       ChangeTypeDeployment,
		IncidentID:       incidentID,
		CorrelationScore: score,
		Reason:           reason,
		TimeDelta:        timeDelta,
	}
}

// GetRecentForService returns recent changes for a specific service.
func (a *ChangeAnalyzer) GetRecentForService(
	ctx context.Context,
	service string,
	since time.Time,
) ([]DeploymentEvent, error) {
	filter := DeploymentFilter{
		Service:   service,
		StartTime: &since,
		Limit:     50,
	}

	return a.store.ListDeployments(ctx, filter)
}
