// Package integrations provides K8s change detection integration.
package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/yourusername/OpsAgent/internal/rca/models"
)

// K8sChangeSource implements ChangeSource using Kubernetes API.
type K8sChangeSource struct {
	client    *http.Client
	endpoint  string
	token     string
	namespace string
}

// NewK8sChangeSource creates a new K8s change source.
func NewK8sChangeSource(endpoint, token, namespace string) (*K8sChangeSource, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("K8s endpoint is required")
	}

	return &K8sChangeSource{
		client:    &http.Client{Timeout: 30 * time.Second},
		endpoint:  endpoint,
		token:     token,
		namespace: namespace,
	}, nil
}

// QueryChanges retrieves changes within the time window.
func (k *K8sChangeSource) QueryChanges(ctx context.Context, start, end time.Time) ([]models.Change, error) {
	ns := k.namespace
	if ns == "" {
		ns = "default"
	}

	url := fmt.Sprintf("%s/apis/apps/v1/namespaces/%s/deployments", k.endpoint, ns)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	k.signRequest(req)

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("K8s deployments request failed: %s", string(body))
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name      string            `json:"name"`
				Namespace string            `json:"namespace"`
				Labels    map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type           string    `json:"type"`
					Status         string    `json:"status"`
					LastUpdateTime time.Time `json:"lastUpdateTime"`
					Reason         string    `json:"reason"`
				} `json:"conditions"`
			} `json:"status"`
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Name  string `json:"name"`
							Image string `json:"image"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	changes := []models.Change{}
	for _, item := range result.Items {
		serviceName := item.Metadata.Labels["app"]
		if serviceName == "" {
			serviceName = item.Metadata.Name
		}

		for _, cond := range item.Status.Conditions {
			if cond.Type == "Progressing" && cond.Status == "True" {
				if cond.LastUpdateTime.After(start) && cond.LastUpdateTime.Before(end) {
					image := ""
					if len(item.Spec.Template.Spec.Containers) > 0 {
						image = item.Spec.Template.Spec.Containers[0].Image
					}

					changes = append(changes, models.Change{
						ID:          fmt.Sprintf("%s-%d", serviceName, cond.LastUpdateTime.Unix()),
						Timestamp:   cond.LastUpdateTime,
						Service:     serviceName,
						Type:        "deployment",
						Description: fmt.Sprintf("Deployment updated: %s", image),
						Details: map[string]string{
							"namespace": item.Metadata.Namespace,
							"reason":    cond.Reason,
							"image":     image,
						},
					})
				}
			}
		}
	}

	return changes, nil
}

// QueryChangesByService retrieves changes for a specific service.
func (k *K8sChangeSource) QueryChangesByService(ctx context.Context, service string, start, end time.Time) ([]models.Change, error) {
	changes, err := k.QueryChanges(ctx, start, end)
	if err != nil {
		return nil, err
	}

	filtered := []models.Change{}
	for _, change := range changes {
		if change.Service == service {
			filtered = append(filtered, change)
		}
	}

	return filtered, nil
}

// GetLastChange returns the most recent change for a service.
func (k *K8sChangeSource) GetLastChange(ctx context.Context, service string) (*models.Change, error) {
	end := time.Now()
	start := end.Add(-24 * time.Hour)

	changes, err := k.QueryChangesByService(ctx, service, start, end)
	if err != nil {
		return nil, err
	}

	if len(changes) == 0 {
		return nil, fmt.Errorf("no changes found for service: %s", service)
	}

	last := &changes[0]
	for _, change := range changes[1:] {
		if change.Timestamp.After(last.Timestamp) {
			last = &change
		}
	}

	return last, nil
}

func (k *K8sChangeSource) signRequest(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+k.token)
}
