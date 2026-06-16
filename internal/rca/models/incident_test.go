// Package models provides tests for RCA data models.
package models

import (
	"testing"
	"time"
)

func TestIncidentValidation(t *testing.T) {
	tests := []struct {
		name     string
		incident *Incident
		wantErr  bool
	}{
		{
			name: "valid incident",
			incident: &Incident{
				ID:              "incident-001",
				DetectedAt:      time.Now(),
				StartTime:       time.Now().Add(-1 * time.Hour),
				EndTime:         time.Now(),
				Severity:        "P0",
				AffectedServices: []string{"service-a", "service-b"},
				Description:     "Service degradation detected",
				Source:          "prometheus-alert",
			},
			wantErr: false,
		},
		{
			name: "empty incident ID",
			incident: &Incident{
				ID:        "",
				StartTime: time.Now().Add(-1 * time.Hour),
				EndTime:   time.Now(),
				Severity:  "P1",
			},
			wantErr: true,
		},
		{
			name: "end time before start time",
			incident: &Incident{
				ID:        "incident-002",
				StartTime: time.Now(),
				EndTime:   time.Now().Add(-1 * time.Hour),
				Severity:  "P0",
			},
			wantErr: true,
		},
		{
			name: "invalid severity",
			incident: &Incident{
				ID:        "incident-003",
				StartTime: time.Now().Add(-1 * time.Hour),
				EndTime:   time.Now(),
				Severity:  "P5",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.incident.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Incident.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTopologyHasService(t *testing.T) {
	tests := []struct {
		name     string
		topology *Topology
		service  string
		want     bool
	}{
		{
			name: "service exists",
			topology: &Topology{
				Nodes: []ServiceNode{
					{Name: "service-a", Type: "application"},
					{Name: "service-b", Type: "database"},
				},
			},
			service: "service-a",
			want:    true,
		},
		{
			name: "service does not exist",
			topology: &Topology{
				Nodes: []ServiceNode{
					{Name: "service-a", Type: "application"},
				},
			},
			service: "service-c",
			want:    false,
		},
		{
			name:     "nil topology",
			topology: nil,
			service:  "service-a",
			want:     false,
		},
		{
			name: "empty topology",
			topology: &Topology{
				Nodes: []ServiceNode{},
			},
			service: "service-a",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.topology.HasService(tt.service)
			if got != tt.want {
				t.Errorf("Topology.HasService() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTopologyGetDependencies(t *testing.T) {
	tests := []struct {
		name     string
		topology *Topology
		service  string
		want     []string
	}{
		{
			name: "service has dependencies",
			topology: &Topology{
				Edges: []DependencyEdge{
					{From: "service-a", To: "service-b", Type: "http"},
					{From: "service-a", To: "service-c", Type: "rpc"},
				},
			},
			service: "service-a",
			want:    []string{"service-b", "service-c"},
		},
		{
			name: "service has no dependencies",
			topology: &Topology{
				Edges: []DependencyEdge{
					{From: "service-b", To: "service-c", Type: "http"},
				},
			},
			service: "service-a",
			want:    nil,
		},
		{
			name:     "nil topology",
			topology: nil,
			service:  "service-a",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.topology.GetDependencies(tt.service)
			if !equalSlices(got, tt.want) {
				t.Errorf("Topology.GetDependencies() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTopologyGetDependents(t *testing.T) {
	tests := []struct {
		name     string
		topology *Topology
		service  string
		want     []string
	}{
		{
			name: "service has dependents",
			topology: &Topology{
				Edges: []DependencyEdge{
					{From: "service-a", To: "service-b", Type: "http"},
					{From: "service-c", To: "service-b", Type: "rpc"},
				},
			},
			service: "service-b",
			want:    []string{"service-a", "service-c"},
		},
		{
			name: "service has no dependents",
			topology: &Topology{
				Edges: []DependencyEdge{
					{From: "service-a", To: "service-b", Type: "http"},
				},
			},
			service: "service-a",
			want:    nil,
		},
		{
			name:     "nil topology",
			topology: nil,
			service:  "service-b",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.topology.GetDependents(tt.service)
			if !equalSlices(got, tt.want) {
				t.Errorf("Topology.GetDependents() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrendString(t *testing.T) {
	tests := []struct {
		name  string
		trend Trend
		want  string
	}{
		{"stable trend", TrendStable, "stable"},
		{"up trend", TrendUp, "up"},
		{"down trend", TrendDown, "down"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.trend.String(); got != tt.want {
				t.Errorf("Trend.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
