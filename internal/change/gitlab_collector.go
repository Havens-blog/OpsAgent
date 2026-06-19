// Package change provides GitLab CI integration for deployment tracking.
package change

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// GitLabCollector collects deployment events from GitLab CI.
type GitLabCollector struct {
	baseURL    string
	token      string
	httpClient *http.Client
	timeout    time.Duration
}

// GitLabCollectorOption configures a GitLabCollector.
type GitLabCollectorOption func(*GitLabCollector)

// WithBaseURL sets the GitLab base URL.
func WithBaseURL(url string) GitLabCollectorOption {
	return func(c *GitLabCollector) {
		c.baseURL = url
	}
}

// WithToken sets the GitLab personal access token.
func WithToken(token string) GitLabCollectorOption {
	return func(c *GitLabCollector) {
		c.token = token
	}
}

// WithTimeout sets the HTTP timeout.
func WithTimeout(timeout time.Duration) GitLabCollectorOption {
	return func(c *GitLabCollector) {
		c.timeout = timeout
	}
}

// NewGitLabCollector creates a new GitLab CI collector.
func NewGitLabCollector(opts ...GitLabCollectorOption) *GitLabCollector {
	c := &GitLabCollector{
		baseURL:    "https://gitlab.com",
		httpClient: &http.Client{},
		timeout:    30 * time.Second,
	}

	for _, opt := range opts {
		opt(c)
	}

	c.httpClient.Timeout = c.timeout

	return c
}

// CollectDeployments collects deployment events from GitLab projects.
func (c *GitLabCollector) CollectDeployments(ctx context.Context, window TimeWindow) ([]DeploymentEvent, error) {
	// In a real implementation, this would:
	// 1. List projects the user has access to
	// 2. Query pipelines for each project
	// 3. Extract deployment jobs from pipelines
	// 4. Return deployment events

	// For now, return mock data for testing
	mockDeployments := []DeploymentEvent{
		{
			ID:           "gitlab-deploy-001",
			CICDSystem:   CICDSystemGitLabCI,
			Project:      "myproject/backend",
			Repository:   "gitlab.com/myproject/backend",
			Environment:  "production",
			Service:      "order-service",
			CommitSHA:    "abc123def456",
			CommitMessage: "Fix: Database connection pool exhaustion",
			CommitAuthor: "John Doe",
			Branch:       "main",
			PipelineID:   "12345",
			JobID:        "deploy-prod",
			StartedAt:    window.StartTime.Add(2 * time.Minute),
			CompletedAt:  window.StartTime.Add(5 * time.Minute),
			Duration:     3 * time.Minute,
			Status:       DeploymentStatusSuccess,
		},
	}

	return mockDeployments, nil
}

// GetLatestDeployment returns the most recent deployment for a service.
func (c *GitLabCollector) GetLatestDeployment(ctx context.Context, service, environment string) (DeploymentEvent, error) {
	// In a real implementation, query GitLab API for the latest deployment
	return DeploymentEvent{}, fmt.Errorf("not implemented")
}

// Health checks if the GitLab API is accessible.
func (c *GitLabCollector) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v4/user", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// GitLab API types

type gitlabPipeline struct {
	ID        int       `json:"id"`
	ProjectID int       `json:"project_id"`
	SHA       string    `json:"sha"`
	Ref       string    `json:"ref"`
	Status    string    `json:"status"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	WebURL    string    `json:"web_url"`
}

type gitlabJob struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Stage      string    `json:"stage"`
	Status     string    `json:"status"`
	PipelineID int      `json:"pipeline_id"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Duration   float64   `json:"duration"`
	Runner     struct {
		ID          int    `json:"id"`
		Description string `json:"description"`
	} `json:"runner"`
}

type gitlabCommit struct {
	ID             string    `json:"id"`
	ShortID        string    `json:"short_id"`
	Title          string    `json:"title"`
	Message        string    `json:"message"`
	AuthorName     string    `json:"author_name"`
	AuthorEmail    string    `json:"author_email"`
	AuthoredDate   time.Time `json:"authored_date"`
	CommittedDate  time.Time `json:"committed_date"`
	WebURL         string    `json:"web_url"`
}

type gitlabDiff struct {
	Diff        string `json:"diff"`
	NewPath     string `json:"new_path"`
	OldPath     string `json:"old_path"`
	AMode       string `json:"a_mode"`
	BMode       string `json:"b_mode"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
}

// Helper methods

func (c *GitLabCollector) addAuthHeaders(req *http.Request) {
	if c.token != "" {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	}
	req.Header.Set("Accept", "application/json")
}

func (c *GitLabCollector) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := c.baseURL + "/api/v4" + path
	if params != nil && len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

func (c *GitLabCollector) getPipelines(ctx context.Context, projectPath string, window TimeWindow) ([]gitlabPipeline, error) {
	encodedPath := url.PathEscape(projectPath)
	params := url.Values{}
	params.Set("updated_after", window.StartTime.Format(time.RFC3339))
	params.Set("updated_before", window.EndTime.Format(time.RFC3339))
	params.Set("order_by", "updated_at")
	params.Set("sort", "desc")
	params.Set("per_page", "100")

	data, err := c.get(ctx, "/projects/"+encodedPath+"/pipelines", params)
	if err != nil {
		return nil, err
	}

	var pipelines []gitlabPipeline
	if err := json.Unmarshal(data, &pipelines); err != nil {
		return nil, err
	}

	return pipelines, nil
}

func (c *GitLabCollector) getJobs(ctx context.Context, projectPath string, pipelineID int) ([]gitlabJob, error) {
	encodedPath := url.PathEscape(projectPath)
	data, err := c.get(ctx, fmt.Sprintf("/projects/%s/pipelines/%d/jobs", encodedPath, pipelineID), nil)
	if err != nil {
		return nil, err
	}

	var jobs []gitlabJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, err
	}

	return jobs, nil
}

func (c *GitLabCollector) getCommit(ctx context.Context, projectPath string, sha string) (gitlabCommit, error) {
	encodedPath := url.PathEscape(projectPath)
	data, err := c.get(ctx, fmt.Sprintf("/projects/%s/repository/commits/%s", encodedPath, sha), nil)
	if err != nil {
		return gitlabCommit{}, err
	}

	var commit gitlabCommit
	if err := json.Unmarshal(data, &commit); err != nil {
		return gitlabCommit{}, err
	}

	return commit, nil
}

func (c *GitLabCollector) getDiff(ctx context.Context, projectPath string, sha string) ([]gitlabDiff, error) {
	encodedPath := url.PathEscape(projectPath)
	data, err := c.get(ctx, fmt.Sprintf("/projects/%s/repository/commits/%s/diff", encodedPath, sha), nil)
	if err != nil {
		return nil, err
	}

	var diffs []gitlabDiff
	if err := json.Unmarshal(data, &diffs); err != nil {
		return nil, err
	}

	return diffs, nil
}

// mapGitLabStatus maps GitLab pipeline status to DeploymentStatus.
func mapGitLabStatus(status string) DeploymentStatus {
	switch status {
	case "success":
		return DeploymentStatusSuccess
	case "failed":
		return DeploymentStatusFailed
	case "running":
		return DeploymentStatusRunning
	case "pending":
		return DeploymentStatusPending
	default:
		return DeploymentStatusFailed
	}
}

// extractEnvironmentFromJob extracts environment from job name.
func extractEnvironmentFromJob(jobName string) string {
	jobName = strings.ToLower(jobName)

	if strings.Contains(jobName, "prod") || strings.Contains(jobName, "production") {
		return "production"
	}
	if strings.Contains(jobName, "stage") || strings.Contains(jobName, "staging") {
		return "staging"
	}
	if strings.Contains(jobName, "dev") || strings.Contains(jobName, "develop") {
		return "development"
	}
	if strings.Contains(jobName, "test") {
		return "test"
	}

	return "unknown"
}

// extractServiceFromJob extracts service name from job name.
func extractServiceFromJob(jobName string) string {
	// Common patterns: deploy-order-service, deploy:order, order:deploy
	parts := strings.Split(strings.ToLower(jobName), "-")
	for _, part := range parts {
		if strings.Contains(part, "service") || strings.Contains(part, "app") {
			// Extract service name
			serviceName := strings.TrimSuffix(part, "service")
			serviceName = strings.TrimSuffix(serviceName, "app")
			return strings.TrimSpace(serviceName)
		}
	}

	// Fall back to the job name with common deploy prefixes stripped.
	// (TrimPrefix handles both "deploy-foo" and "deploy:foo".)
	name := strings.TrimPrefix(jobName, "deploy-")
	name = strings.TrimPrefix(name, "deploy:")
	return name
}

// parseChangesFromDiff parses deployment changes from GitLab diff.
func parseChangesFromDiff(diffs []gitlabDiff) []DeploymentChange {
	changes := make([]DeploymentChange, 0, len(diffs))

	for _, diff := range diffs {
		changeType := "modified"
		if diff.NewFile {
			changeType = "added"
		} else if diff.DeletedFile {
			changeType = "deleted"
		}

		change := DeploymentChange{
			Type: changeType,
			Path: diff.NewPath,
		}

		if diff.OldPath != "" && diff.OldPath != diff.NewPath {
			// Renamed file
			change.OldSHA = diff.OldPath
			change.NewSHA = diff.NewPath
		}

		changes = append(changes, change)
	}

	// Sort by path for consistent ordering
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Path < changes[j].Path
	})

	return changes
}

// GitLabProject represents a GitLab project configuration.
type GitLabProject struct {
	Path      string `json:"path"`      // e.g., "group/project"
	Enabled   bool   `json:"enabled"`
	Environments []string `json:"environments"` // Environments to monitor
}

// GitLabConfig represents GitLab collector configuration.
type GitLabConfig struct {
	BaseURL  string          `json:"base_url"`
	Token    string          `json:"token"`
	Projects []GitLabProject `json:"projects"`
	Timeout  time.Duration   `json:"timeout"`
}

// Validate validates the GitLab configuration.
func (c *GitLabConfig) Validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	if c.Token == "" {
		return fmt.Errorf("token is required")
	}
	if len(c.Projects) == 0 {
		return fmt.Errorf("at least one project is required")
	}

	// Validate project URLs
	for _, project := range c.Projects {
		if project.Path == "" {
			return fmt.Errorf("project path is required")
		}
	}

	return nil
}
