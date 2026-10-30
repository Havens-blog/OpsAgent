// Package agent provides data source factory functions for RCA integration.
package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// TestBuildLogSourcesECamFederation verifies the e-cam federation wiring:
// configured + reachable => first named source; unreachable => skipped.
func TestBuildLogSourcesECamFederation(t *testing.T) {
	// 可达的 e-cam 假端点(types 探针 + search/aggregate 都通)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/cam/logs/types":
			_, _ = w.Write([]byte(`{"code":0,"data":[{"type":"app"}]}`))
		case "/api/v1/cam/logs/search":
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":1,"entries":[{"timestamp":1,"service":"s","level":"error","message":"m"}]}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":0}}`))
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &DataSourceConfig{
		ECamEndpoint: srv.URL,
		ECamToken:    "tok",
		ECamTimeout:  (3 * time.Second).String(),
	}
	got := buildLogSources(cfg, nil)
	if got == nil {
		t.Fatalf("e-cam 配置 + 可达应产出日志源")
	}
	agg, ok := got.(*sources.AggregateLogSource)
	if !ok || agg.Count() != 1 {
		t.Fatalf("应为一个聚合源(仅 e-cam), got %T", got)
	}

	// 不可达端点:跳过(渐进增强,不阻塞启动)
	dead := buildLogSources(&DataSourceConfig{ECamEndpoint: "http://127.0.0.1:1", ECamToken: "t"}, nil)
	if dead != nil {
		t.Fatalf("e-cam 不可达应跳过, got %v", dead)
	}
}

// TestDataSourceConfigIsEmptyECam IsEmpty 必须感知 e-cam 配置,
// 否则"仅配 e-cam"的部署会被当成空配置退回 mock。
func TestDataSourceConfigIsEmptyECam(t *testing.T) {
	if !(&DataSourceConfig{}).IsEmpty() {
		t.Fatalf("全空配置应 IsEmpty")
	}
	if (&DataSourceConfig{ECamEndpoint: "http://127.0.0.1:8001"}).IsEmpty() {
		t.Fatalf("仅配 e-cam 不应视为空")
	}
}

// TestNewDataSourceConfigFromEnvECam 环境变量接线(凭据只从环境读取,不落盘)。
func TestNewDataSourceConfigFromEnvECam(t *testing.T) {
	t.Setenv("ECAM_LOGQUERY_ENDPOINT", "http://127.0.0.1:8001")
	t.Setenv("ECAM_LOGQUERY_TOKEN", "jwt")
	t.Setenv("ECAM_LOGQUERY_LOG_TYPE", "app")
	t.Setenv("ECAM_LOGQUERY_TIMEOUT", "10s")
	cfg := NewDataSourceConfigFromEnv()
	if cfg.ECamEndpoint != "http://127.0.0.1:8001" || cfg.ECamToken != "jwt" ||
		cfg.ECamLogType != "app" || cfg.ECamTimeout != "10s" {
		t.Fatalf("env 未正确接线: %+v", cfg)
	}
}
