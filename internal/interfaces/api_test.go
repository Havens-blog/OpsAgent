// Package interfaces provides tests for REST API handlers.
package interfaces

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
)

// TestAPIServerCreation tests API server creation.
func TestAPIServerCreation(t *testing.T) {
	t.Run("creates_server_successfully", func(t *testing.T) {
		settings := &config.Settings{
			Environment: config.EnvDevelopment,
			Server: config.ServerConfig{
				Host: "localhost",
				Port: 8080,
			},
		}

		server, err := NewAPIServer(settings)
		if err != nil {
			t.Fatalf("Failed to create API server: %v", err)
		}
		defer server.Shutdown(context.Background())

		if server == nil {
			t.Fatal("Expected non-nil server")
		}
	})
}

// TestHealthEndpoint tests the health check endpoint.
func TestHealthEndpoint(t *testing.T) {
	settings := &config.Settings{
		Environment: config.EnvDevelopment,
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}

	server, err := NewAPIServer(settings)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}
	defer server.Shutdown(context.Background())

	t.Run("health_check_returns_200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response HealthResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if response.Status != "healthy" {
			t.Errorf("Expected status 'healthy', got '%s'", response.Status)
		}
	})
}

// TestRCAEndpoint tests the RCA analysis endpoint.
func TestRCAEndpoint(t *testing.T) {
	settings := &config.Settings{
		Environment: config.EnvDevelopment,
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}

	server, err := NewAPIServer(settings)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}
	defer server.Shutdown(context.Background())

	t.Run("valid_rca_request_returns_200", func(t *testing.T) {
		request := RCAAnalysisRequest{
			IncidentID: "INC-001",
			StartTime: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			EndTime:   time.Now().Format(time.RFC3339),
			Severity:  "P1",
			AffectedServices: []string{"order-service"},
		}

		body, _ := json.Marshal(request)
		req := httptest.NewRequest("POST", "/api/v1/rca/analyze", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		// Should process request (may return 202 for async processing)
		if w.Code != http.StatusOK && w.Code != http.StatusAccepted {
			t.Errorf("Expected status 200 or 202, got %d", w.Code)
		}
	})

	t.Run("invalid_request_returns_400", func(t *testing.T) {
		request := RCAAnalysisRequest{
			// Missing required fields
		}

		body, _ := json.Marshal(request)
		req := httptest.NewRequest("POST", "/api/v1/rca/analyze", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})
}

// TestLogQueryEndpoint tests the log query endpoint.
func TestLogQueryEndpoint(t *testing.T) {
	settings := &config.Settings{
		Environment: config.EnvDevelopment,
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}

	server, err := NewAPIServer(settings)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}
	defer server.Shutdown(context.Background())

	t.Run("log_query_request", func(t *testing.T) {
		request := LogQueryRequest{
			Query:     "level:error",
			StartTime: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			EndTime:   time.Now().Format(time.RFC3339),
			Limit:     100,
		}

		body, _ := json.Marshal(request)
		req := httptest.NewRequest("POST", "/api/v1/logs/query", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK && w.Code != http.StatusAccepted {
			t.Errorf("Expected status 200 or 202, got %d", w.Code)
		}
	})
}

// TestMetricsQueryEndpoint tests the metrics query endpoint.
func TestMetricsQueryEndpoint(t *testing.T) {
	settings := &config.Settings{
		Environment: config.EnvDevelopment,
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}

	server, err := NewAPIServer(settings)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}
	defer server.Shutdown(context.Background())

	t.Run("metrics_query_request", func(t *testing.T) {
		request := MetricsQueryRequest{
			Query:     "http_requests_total",
			StartTime: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			EndTime:   time.Now().Format(time.RFC3339),
			Step:      "1m",
		}

		body, _ := json.Marshal(request)
		req := httptest.NewRequest("POST", "/api/v1/metrics/query", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK && w.Code != http.StatusAccepted {
			t.Errorf("Expected status 200 or 202, got %d", w.Code)
		}
	})
}

// TestAPIResponseFormat tests API response format consistency.
func TestAPIResponseFormat(t *testing.T) {
	settings := &config.Settings{
		Environment: config.EnvDevelopment,
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}

	server, err := NewAPIServer(settings)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}
	defer server.Shutdown(context.Background())

	t.Run("error_response_format", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/invalid", nil)
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var response APIResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if response.Success {
			t.Error("Expected success to be false for invalid endpoint")
		}

		if response.Error == nil {
			t.Error("Expected error message in response")
		}
	})
}

// TestCORSMiddleware tests CORS middleware.
func TestCORSMiddleware(t *testing.T) {
	settings := &config.Settings{
		Environment: config.EnvDevelopment,
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}

	server, err := NewAPIServer(settings)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}
	defer server.Shutdown(context.Background())

	t.Run("options_request_returns_cors_headers", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/api/v1/rca/analyze", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 for OPTIONS, got %d", w.Code)
		}

		corsHeader := w.Header().Get("Access-Control-Allow-Origin")
		if corsHeader == "" {
			t.Error("Expected CORS headers to be set")
		}
	})
}

// Request/Response types are defined in api.go
// Local test-specific types:

type HealthResponse struct {
	Status    string                 `json:"status"`
	Timestamp string                 `json:"timestamp"`
	Version   string                 `json:"version"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

type APIResponse struct {
	Success bool                   `json:"success"`
	Data    interface{}            `json:"data,omitempty"`
	Error   *ErrorResponse        `json:"error,omitempty"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}
