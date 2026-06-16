// Package integrations provides ARMS topology integration.
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

// ARMSTopologySource implements TopologySource using Alibaba Cloud ARMS.
type ARMSTopologySource struct {
	client    *http.Client
	endpoint  string
	pid       string
	accessKey string
	secretKey string
}

// NewARMSTopologySource creates a new ARMS topology source.
func NewARMSTopologySource(endpoint, pid, accessKey, secretKey string) (*ARMSTopologySource, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("ARMS endpoint is required")
	}
	if pid == "" {
		return nil, fmt.Errorf("ARMS pid is required")
	}

	return &ARMSTopologySource{
		client:    &http.Client{Timeout: 30 * time.Second},
		endpoint:  endpoint,
		pid:       pid,
		accessKey: accessKey,
		secretKey: secretKey,
	}, nil
}

// GetTopology retrieves the current service dependency graph from ARMS.
func (a *ARMSTopologySource) GetTopology(ctx context.Context) (*models.Topology, error) {
	url := fmt.Sprintf("%s/api/v1/arms/topology?Pid=%s", a.endpoint, a.pid)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	a.signRequest(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ARMS topology request failed: %s", string(body))
	}

	var result struct {
		Data struct {
			Nodes []struct {
				Name   string            `json:"name"`
				Type   string            `json:"type"`
				Labels map[string]string `json:"labels"`
			} `json:"nodes"`
			Edges []struct {
				From string `json:"from"`
				To   string `json:"to"`
				Type string `json:"type"`
			} `json:"edges"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	topology := &models.Topology{
		Nodes: make([]models.ServiceNode, 0, len(result.Data.Nodes)),
		Edges: make([]models.DependencyEdge, 0, len(result.Data.Edges)),
	}

	for _, n := range result.Data.Nodes {
		topology.Nodes = append(topology.Nodes, models.ServiceNode{
			Name:   n.Name,
			Type:   n.Type,
			Labels: n.Labels,
		})
	}

	for _, e := range result.Data.Edges {
		topology.Edges = append(topology.Edges, models.DependencyEdge{
			From: e.From,
			To:   e.To,
			Type: e.Type,
		})
	}

	return topology, nil
}

// GetServiceNeighbors returns services that depend on or are depended by the given service.
func (a *ARMSTopologySource) GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error) {
	topo, err := a.GetTopology(ctx)
	if err != nil {
		return nil, nil, err
	}

	dependencies = topo.GetDependencies(service)
	dependents = topo.GetDependents(service)

	return dependents, dependencies, nil
}

// GetServiceHealth returns health status for services in the topology.
func (a *ARMSTopologySource) GetServiceHealth(ctx context.Context) (map[string]string, error) {
	url := fmt.Sprintf("%s/api/v1/arms/health?Pid=%s", a.endpoint, a.pid)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	a.signRequest(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ARMS health request failed: %d", resp.StatusCode)
	}

	var result struct {
		Data map[string]string `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

func (a *ARMSTopologySource) signRequest(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}
