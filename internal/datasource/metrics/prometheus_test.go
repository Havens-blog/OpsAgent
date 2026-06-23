package metrics

import (
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
)

func TestNewPrometheusClient(t *testing.T) {
	tests := []struct {
		name    string
		config  *core.PrometheusConfig
		wantErr bool
	}{
		{
			name: "valid config with bearer token",
			config: &core.PrometheusConfig{
				Config: core.Config{
					Name:    "test-prom",
					Type:    core.DataSourceTypePrometheus,
					Enabled: true,
					Timeout: 30 * time.Second,
				},
				Endpoint:    "http://localhost:9090",
				BearerToken: "test-token",
			},
			wantErr: false,
		},
		{
			name: "valid config with basic auth",
			config: &core.PrometheusConfig{
				Config: core.Config{
					Name:    "test-prom",
					Type:    core.DataSourceTypePrometheus,
					Enabled: true,
					Timeout: 30 * time.Second,
				},
				Endpoint: "http://localhost:9090",
				Username: "admin",
				Password: "password",
			},
			wantErr: false,
		},
		{
			name: "missing endpoint",
			config: &core.PrometheusConfig{
				Config: core.Config{
					Name:    "test-prom",
					Type:    core.DataSourceTypePrometheus,
					Enabled: true,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewPrometheusClient(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPrometheusClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("NewPrometheusClient() returned nil client")
			}
		})
	}
}

func TestPrometheusClient_Type(t *testing.T) {
	config := &core.PrometheusConfig{
		Config: core.Config{
			Name:    "test-prom",
			Type:    core.DataSourceTypePrometheus,
			Enabled: true,
		},
		Endpoint: "http://localhost:9090",
	}

	client, err := NewPrometheusClient(config)
	if err != nil {
		t.Fatalf("NewPrometheusClient() error = %v", err)
	}

	if client.Type() != core.DataSourceTypePrometheus {
		t.Errorf("Type() = %v, want %v", client.Type(), core.DataSourceTypePrometheus)
	}
}

func TestPrometheusClient_Name(t *testing.T) {
	config := &core.PrometheusConfig{
		Config: core.Config{
			Name:    "test-prom",
			Type:    core.DataSourceTypePrometheus,
			Enabled: true,
		},
		Endpoint: "http://localhost:9090",
	}

	client, err := NewPrometheusClient(config)
	if err != nil {
		t.Fatalf("NewPrometheusClient() error = %v", err)
	}

	if client.Name() != "test-prom" {
		t.Errorf("Name() = %v, want %v", client.Name(), "test-prom")
	}
}

func TestBaseMetricsDataSource_ValidateMetricsQueryRequest(t *testing.T) {
	base := NewBaseMetricsDataSource(&core.Config{
		Name:    "test",
		Type:    core.DataSourceTypePrometheus,
		Enabled: true,
	})

	tests := []struct {
		name    string
		req     *core.MetricsQueryRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty query",
			req: &core.MetricsQueryRequest{
				StartTime: time.Now().Add(-1 * time.Hour),
				EndTime:   time.Now(),
				Query:     "",
			},
			wantErr: true,
		},
		{
			name: "missing start time",
			req: &core.MetricsQueryRequest{
				EndTime: time.Now(),
				Query:   "up",
			},
			wantErr: true,
		},
		{
			name: "missing end time",
			req: &core.MetricsQueryRequest{
				StartTime: time.Now().Add(-1 * time.Hour),
				Query:     "up",
			},
			wantErr: true,
		},
		{
			name: "end time before start time",
			req: &core.MetricsQueryRequest{
				StartTime: time.Now(),
				EndTime:   time.Now().Add(-1 * time.Hour),
				Query:     "up",
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &core.MetricsQueryRequest{
				StartTime: time.Now().Add(-1 * time.Hour),
				EndTime:   time.Now(),
				Query:     "up",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := base.ValidateMetricsQueryRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMetricsQueryRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPrometheusClient_Close(t *testing.T) {
	config := &core.PrometheusConfig{
		Config: core.Config{
			Name:    "test-prom",
			Type:    core.DataSourceTypePrometheus,
			Enabled: true,
		},
		Endpoint: "http://localhost:9090",
	}

	client, err := NewPrometheusClient(config)
	if err != nil {
		t.Fatalf("NewPrometheusClient() error = %v", err)
	}

	// Should not panic
	err = client.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}
