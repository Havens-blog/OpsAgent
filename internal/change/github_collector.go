// Package change provides GitHub Actions integration for deployment tracking.
package change

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHubCollector collects deployment events from GitHub Actions.
type GitHubCollector struct {
	baseURL    string
	token      string
	httpClient *http.Client
	timeout    time.Duration
}

// GitHubCollectorOption configures a GitHubCollector.
type GitHubCollectorOption func(*GitHubCollector)

// WithGitHubBaseURL sets the GitHub base URL.
func WithGitHubBaseURL(url string) GitHubCollectorOption {
	return func(c *GitHubCollector) {
		c.baseURL = url
	}
}

// WithGitHubToken sets the GitHub personal access token.
func WithGitHubToken(token string) GitHubCollectorOption {
	return func(c *GitHubCollector) {
		c.token = token
	}
}

// WithGitHubTimeout sets the HTTP timeout.
func WithGitHubTimeout(timeout time.Duration) GitHubCollectorOption {
	return func(c *GitHubCollector) {
		c.timeout = timeout
	}
}

// NewGitHubCollector creates a new GitHub Actions collector.
func NewGitHubCollector(opts ...GitHubCollectorOption) *GitHubCollector {
	c := &GitHubCollector{
		baseURL:    "https://api.github.com",
		httpClient: &http.Client{},
		timeout:    30 * time.Second,
	}

	for _, opt := range opts {
		opt(c)
	}

	c.httpClient.Timeout = c.timeout

	return c
}

// CollectDeployments collects deployment events from GitHub repositories.
func (c *GitHubCollector) CollectDeployments(ctx context.Context, window TimeWindow) ([]DeploymentEvent, error) {
	// In a real implementation, this would:
	// 1. List repositories the user has access to
	// 2. Query workflow runs for each repository
	// 3. Extract deployment information from workflow runs
	// 4. Return deployment events

	// For now, return mock data for testing
	mockDeployments := []DeploymentEvent{
		{
			ID:           "github-deploy-001",
			CICDSystem:   CICDSystemGitHubActions,
			Project:      "myorg/backend",
			Repository:   "github.com/myorg/backend",
			Environment:  "production",
			Service:      "payment-service",
			CommitSHA:    "def456ghi789",
			CommitMessage: "feat: Add payment gateway integration",
			CommitAuthor: "Jane Smith",
			Branch:       "main",
			PipelineID:   "54321",
			JobID:        "deploy-production.yml",
			StartedAt:    window.StartTime.Add(10 * time.Minute),
			CompletedAt:  window.StartTime.Add(15 * time.Minute),
			Duration:     5 * time.Minute,
			Status:       DeploymentStatusSuccess,
		},
	}

	return mockDeployments, nil
}

// GetLatestDeployment returns the most recent deployment for a service.
func (c *GitHubCollector) GetLatestDeployment(ctx context.Context, service, environment string) (DeploymentEvent, error) {
	// In a real implementation, query GitHub API for the latest deployment
	return DeploymentEvent{}, fmt.Errorf("not implemented")
}

// Health checks if the GitHub API is accessible.
func (c *GitHubCollector) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/user", nil)
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

// GitHub API types

type githubWorkflowRun struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	NodeID           string    `json:"node_id"`
	Path             string    `json:"path"`
	Status           string    `json:"status"`
	Conclusion       string    `json:"conclusion"`
	HeadBranch       string    `json:"head_branch"`
	HeadSHA          string    `json:"head_sha"`
	RunStartedAt     time.Time `json:"run_started_at"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Event            string    `json:"event"`
	DisplayTitle     string    `json:"display_title"`
	RunNumber        int       `json:"run_number"`
	RunAttempt       int       `json:"run_attempt"`
	WorkflowID       int       `json:"workflow_id"`
	HeadRepository   struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		FullName string `json:"full_name"`
	} `json:"head_repository"`
}

type githubWorkflowJob struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	RunnerName  string    `json:"runner_name"`
	RunnerGroup string    `json:"runner_group"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Steps       []struct {
		Name        string `json:"name"`
		Status      string `json:"status"`
		Conclusion  string `json:"conclusion"`
		Number      int    `json:"number"`
		StartedAt   time.Time `json:"started_at"`
		CompletedAt time.Time `json:"completed_at"`
	} `json:"steps"`
}

type githubCommit struct {
	SHA       string `json:"sha"`
	NodeID    string `json:"node_id"`
	Commit    struct {
		Message   string `json:"message"`
		Author    struct {
			Name  string    `json:"name"`
			Email string    `json:"email"`
			Date  time.Time `json:"date"`
		} `json:"author"`
		Committer struct {
			Name  string    `json:"name"`
			Email string    `json:"email"`
			Date  time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
	Author struct {
		Login     string `json:"login"`
		Type      string `json:"type"`
		SiteAdmin bool   `json:"site_admin"`
	} `json:"author"`
}

type githubDeployment struct {
	ID             int64     `json:"id"`
	SHA            string    `json:"sha"`
	Ref            string    `json:"ref"`
	Task           string    `json:"task"`
	Payload        json.RawMessage `json:"payload"`
	Environment    string    `json:"environment"`
	Description    string    `json:"description"`
	Creator        struct {
		Login string `json:"login"`
	} `json:"creator"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	StatusesURL    string    `json:"statuses_url"`
	RepositoryURL  string    `json:"repository_url"`
	TranscendentEnvironment bool `json:"transcendent_environment"`
	ProductionEnvironment bool `json:"production_environment"`
}

type githubDeploymentStatus struct {
	ID             int64     `json:"id"`
	NodeID         string    `json:"node_id"`
	DeploymentID   int64     `json:"deployment_id"`
	RepositoryID   int64     `json:"repository_id"`
	State          string    `json:"state"`
	Creator        struct {
		Login string `json:"login"`
	} `json:"creator"`
	Description    string    `json:"description"`
	Environment     string    `json:"environment"`
	TargetURL      string    `json:"target_url"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Helper methods

func (c *GitHubCollector) addAuthHeaders(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

func (c *GitHubCollector) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := c.baseURL + path
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

func (c *GitHubCollector) getWorkflowRuns(ctx context.Context, owner, repo string, window TimeWindow) ([]githubWorkflowRun, error) {
	params := url.Values{}
	params.Set("created", fmt.Sprintf("%s..%s",
		window.StartTime.Format(time.RFC3339),
		window.EndTime.Format(time.RFC3339)))

	data, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/actions/runs", owner, repo), params)
	if err != nil {
		return nil, err
	}

	var response struct {
		TotalCount   int                   `json:"total_count"`
		WorkflowRuns []githubWorkflowRun  `json:"workflow_runs"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}

	return response.WorkflowRuns, nil
}

func (c *GitHubCollector) getWorkflowJobs(ctx context.Context, owner, repo string, runID int64) ([]githubWorkflowJob, error) {
	data, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs", owner, repo, runID), nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		TotalCount int                `json:"total_count"`
		Jobs       []githubWorkflowJob `json:"jobs"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}

	return response.Jobs, nil
}

func (c *GitHubCollector) getCommit(ctx context.Context, owner, repo, sha string) (githubCommit, error) {
	data, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/commits/%s", owner, repo, sha), nil)
	if err != nil {
		return githubCommit{}, err
	}

	var commit githubCommit
	if err := json.Unmarshal(data, &commit); err != nil {
		return githubCommit{}, err
	}

	return commit, nil
}

func (c *GitHubCollector) getDeployments(ctx context.Context, owner, repo string, window TimeWindow) ([]githubDeployment, error) {
	params := url.Values{}
	params.Set("since", window.StartTime.Format(time.RFC3339))

	data, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/deployments", owner, repo), params)
	if err != nil {
		return nil, err
	}

	var deployments []githubDeployment
	if err := json.Unmarshal(data, &deployments); err != nil {
		return nil, err
	}

	return deployments, nil
}

func (c *GitHubCollector) getDeploymentStatuses(ctx context.Context, owner, repo string, deploymentID int64) ([]githubDeploymentStatus, error) {
	data, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/deployments/%d/statuses", owner, repo, deploymentID), nil)
	if err != nil {
		return nil, err
	}

	var statuses []githubDeploymentStatus
	if err := json.Unmarshal(data, &statuses); err != nil {
		return nil, err
	}

	return statuses, nil
}

// mapGitHubStatus maps GitHub workflow/conclusion status to DeploymentStatus.
func mapGitHubStatus(conclusion string) DeploymentStatus {
	switch conclusion {
	case "success":
		return DeploymentStatusSuccess
	case "failure":
		return DeploymentStatusFailed
	default:
		return DeploymentStatusFailed
	}
}

// extractEnvironmentFromWorkflow extracts environment from workflow name or path.
func extractEnvironmentFromWorkflow(workflowName string) string {
	name := strings.ToLower(workflowName)

	if strings.Contains(name, "prod") || strings.Contains(name, "production") {
		return "production"
	}
	if strings.Contains(name, "stage") || strings.Contains(name, "staging") {
		return "staging"
	}
	if strings.Contains(name, "dev") || strings.Contains(name, "develop") {
		return "development"
	}
	if strings.Contains(name, "test") {
		return "test"
	}

	return "unknown"
}

// extractServiceFromWorkflow extracts service name from workflow name.
func extractServiceFromWorkflow(workflowName string) string {
	// Common patterns: deploy-order-service.yml, deploy-order.yml, order-deploy
	name := strings.ToLower(workflowName)

	// Remove file extension
	name = strings.TrimSuffix(name, ".yml")
	name = strings.TrimSuffix(name, ".yaml")

	// Extract service name from common patterns
	if strings.HasPrefix(name, "deploy-") {
		service := strings.TrimPrefix(name, "deploy-")
		return strings.TrimSuffix(service, "-service")
	}

	if strings.HasSuffix(name, "-deploy") {
		service := strings.TrimSuffix(name, "-deploy")
		return strings.TrimSuffix(service, "-service")
	}

	return name
}

// GitHubRepository represents a GitHub repository configuration.
type GitHubRepository struct {
	Owner       string   `json:"owner"`        // e.g., "myorg"
	Name        string   `json:"name"`         // e.g., "backend"
	Enabled     bool     `json:"enabled"`
	Environments []string `json:"environments"` // Environments to monitor
	Workflows   []string `json:"workflows"`    // Workflow files to monitor
}

// GitHubConfig represents GitHub collector configuration.
type GitHubConfig struct {
	BaseURL     string              `json:"base_url"`
	Token       string              `json:"token"`
	Repositories []GitHubRepository `json:"repositories"`
	Timeout     time.Duration       `json:"timeout"`
}

// Validate validates the GitHub configuration.
func (c *GitHubConfig) Validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	if c.Token == "" {
		return fmt.Errorf("token is required")
	}
	if len(c.Repositories) == 0 {
		return fmt.Errorf("at least one repository is required")
	}

	// Validate repository configuration
	for _, repo := range c.Repositories {
		if repo.Owner == "" {
			return fmt.Errorf("repository owner is required")
		}
		if repo.Name == "" {
			return fmt.Errorf("repository name is required")
		}
	}

	return nil
}

// ConvertToDeploymentEvent converts a GitHub deployment to a DeploymentEvent.
func ConvertToDeploymentEvent(deployment githubDeployment, statuses []githubDeploymentStatus) DeploymentEvent {
	// Find the latest status
	var latestStatus *githubDeploymentStatus
	for _, status := range statuses {
		if latestStatus == nil || status.UpdatedAt.After(latestStatus.UpdatedAt) {
			latestStatus = &status
		}
	}

	status := DeploymentStatusSuccess
	if latestStatus != nil {
		status = mapGitHubDeploymentStatus(latestStatus.State)
	}

	event := DeploymentEvent{
		ID:          fmt.Sprintf("github-deploy-%d", deployment.ID),
		CICDSystem:  CICDSystemGitHubActions,
		Project:     deployment.RepositoryURL,
		Environment: deployment.Environment,
		Service:     extractServiceFromRef(deployment.Ref),
		CommitSHA:   deployment.SHA,
		Branch:         deployment.Ref,
		StartedAt:   deployment.CreatedAt,
		CompletedAt: deployment.UpdatedAt,
		Status:      status,
	}

	if deployment.ProductionEnvironment {
		event.Annotations = map[string]string{
			"production": "true",
		}
	}

	return event
}

// mapGitHubDeploymentStatus maps GitHub deployment status to DeploymentStatus.
func mapGitHubDeploymentStatus(state string) DeploymentStatus {
	switch state {
	case "success":
		return DeploymentStatusSuccess
	case "failure", "error":
		return DeploymentStatusFailed
	case "in_progress", "pending", "queued":
		return DeploymentStatusRunning
	default:
		return DeploymentStatusPending
	}
}

// extractServiceFromRef extracts service name from git ref.
func extractServiceFromRef(ref string) string {
	// Remove refs/heads/ or refs/tags/ prefix
	ref = strings.TrimPrefix(ref, "refs/heads/")
	ref = strings.TrimPrefix(ref, "refs/tags/")
	ref = strings.TrimPrefix(ref, "refs/heads/")

	// Extract from patterns like feature/order-service or hotfix/payment
	parts := strings.Split(ref, "/")
	if len(parts) > 1 {
		// Last part might be the service
		service := parts[len(parts)-1]
		return strings.TrimSuffix(service, "-service")
	}

	return ref
}
