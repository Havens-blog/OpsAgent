// Package agent provides data source factory functions for RCA integration.
package agent

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	dscore "github.com/Havens-blog/OpsAgent/internal/datasource/core"
	dslog "github.com/Havens-blog/OpsAgent/internal/datasource/log"
	"github.com/Havens-blog/OpsAgent/internal/rca/integrations"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// NewDataSourceConfigFromEnv builds a DataSourceConfig from environment
// variables. Each dimension activates only when its endpoint is present, so an
// unset environment leaves everything empty (callers then fall back to mock).
//
// Recognized variables:
//
//	Prometheus (metrics):
//	  PROMETHEUS_ENDPOINT          base URL, e.g. https://cn-hangzhou.arms.aliyuncs.com/prometheus/.../api/v1 parent
//	  ARMS_PROMETHEUS_BEARER_TOKEN optional bearer token for managed Prometheus
//	ARMS APM topology (preferred — bearer token, no AK/SK):
//	  ARMS_APM_ENDPOINT            ARMS APM Prometheus workspace base URL
//	                               (arms-metrics-* workspace; code appends /api/v1)
//	  ARMS_APM_TOKEN               bearer token for that endpoint
//	ARMS OpenAPI topology (legacy — requires AK/SK signing):
//	  ARMS_ENDPOINT                ARMS OpenAPI endpoint
//	  ARMS_PID                     target application PID
//	  ALIYUN_ACCESS_KEY_ID / ALIYUN_ACCESS_KEY_SECRET  signing credentials
//	Kubernetes (changes):
//	  K8S_ENDPOINT                 API server base URL
//	  K8S_TOKEN                    service-account bearer token
//	  K8S_NAMESPACE                namespace (default "default")
//	e-cam log federation (logs — multi-cloud, consumes the e-cam service):
//	  ECAM_LOGQUERY_ENDPOINT       e-cam base URL, e.g. http://127.0.0.1:8001
//	  ECAM_LOGQUERY_TOKEN          e-cam service bearer token (HS256 JWT minted
//	                               by e-cam cmd/mktok; tenant lives in claims)
//	  ECAM_LOGQUERY_LOG_TYPE       federation log type (default "app" =
//	                               application business logs)
//	  ECAM_LOGQUERY_TIMEOUT        per-request timeout (default "30s")
func NewDataSourceConfigFromEnv() *DataSourceConfig {
	return &DataSourceConfig{
		PrometheusEndpoint: os.Getenv("PROMETHEUS_ENDPOINT"),
		PrometheusToken:    os.Getenv("ARMS_PROMETHEUS_BEARER_TOKEN"),
		ARMSAPMEndpoint:    os.Getenv("ARMS_APM_ENDPOINT"),
		ARMSAPMToken:       os.Getenv("ARMS_APM_TOKEN"),
		ARMSEndpoint:       os.Getenv("ARMS_ENDPOINT"),
		ARMSPid:            os.Getenv("ARMS_PID"),
		ARMSAccessKey:      os.Getenv("ALIYUN_ACCESS_KEY_ID"),
		ARMSSecretKey:      os.Getenv("ALIYUN_ACCESS_KEY_SECRET"),
		K8sEndpoint:        os.Getenv("K8S_ENDPOINT"),
		K8sToken:           os.Getenv("K8S_TOKEN"),
		K8sNamespace:       os.Getenv("K8S_NAMESPACE"),
		ECamEndpoint:       os.Getenv("ECAM_LOGQUERY_ENDPOINT"),
		ECamToken:          os.Getenv("ECAM_LOGQUERY_TOKEN"),
		ECamTokenURL:       os.Getenv("ECAM_LOGQUERY_TOKEN_URL"),
		ECamClientID:       os.Getenv("ECAM_LOGQUERY_CLIENT_ID"),
		ECamClientSecret:   os.Getenv("ECAM_LOGQUERY_CLIENT_SECRET"),
		ECamTenantID:       envInt64("ECAM_LOGQUERY_TENANT_ID"),
		ECamLogType:        os.Getenv("ECAM_LOGQUERY_LOG_TYPE"),
		ECamTimeout:        os.Getenv("ECAM_LOGQUERY_TIMEOUT"),
		LogSources:         logSourcesFromEnv(),
	}
}

// envInt64 读取环境变量并解析为 int64，缺省/非法返回 0。
func envInt64(name string) int64 {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

// logSourcesFromEnv assembles the multi-cloud log source list from the
// environment. It recognizes one Alibaba Cloud SLS and one Volcengine TLS
// backend (the common case); configuring more than one of each is done via the
// config file, not env. Each backend activates only when its required fields
// are present, so an unset environment yields no log sources (=> mock).
//
// Recognized variables:
//
//	Alibaba Cloud SLS (logs):
//	  SLS_ENDPOINT, SLS_PROJECT, SLS_LOGSTORE
//	  SLS_ACCESS_KEY_ID / SLS_ACCESS_KEY_SECRET (fall back to ALIYUN_*)
//	  SLS_SERVICE_FIELD (optional; default "service")
//	  SLS_LEVEL_FIELD (optional; default "level")
//	Volcengine TLS (logs):
//	  TLS_ENDPOINT, TLS_REGION, TLS_TOPIC_ID
//	  TLS_ACCESS_KEY_ID / TLS_ACCESS_KEY_SECRET (fall back to VOLC_*)
//	  TLS_SERVICE_FIELD (optional; default "service")
//	  TLS_LEVEL_FIELD (optional; default "level")
func logSourcesFromEnv() []LogSourceConfig {
	var out []LogSourceConfig

	if ep := os.Getenv("SLS_ENDPOINT"); ep != "" && os.Getenv("SLS_PROJECT") != "" {
		ak := firstNonEmpty(os.Getenv("SLS_ACCESS_KEY_ID"), os.Getenv("ALIYUN_ACCESS_KEY_ID"))
		sk := firstNonEmpty(os.Getenv("SLS_ACCESS_KEY_SECRET"), os.Getenv("ALIYUN_ACCESS_KEY_SECRET"))
		out = append(out, LogSourceConfig{
			Name:            firstNonEmpty(os.Getenv("SLS_NAME"), "aliyun-sls"),
			Type:            "aliyun_sls",
			Endpoint:        ep,
			Project:         os.Getenv("SLS_PROJECT"),
			LogStore:        os.Getenv("SLS_LOGSTORE"),
			AccessKeyID:     ak,
			AccessKeySecret: sk,
			SecurityToken:   os.Getenv("SLS_SECURITY_TOKEN"),
			ServiceField:    os.Getenv("SLS_SERVICE_FIELD"),
			LevelField:      os.Getenv("SLS_LEVEL_FIELD"),
		})
	}

	if ep := os.Getenv("TLS_ENDPOINT"); ep != "" && os.Getenv("TLS_TOPIC_ID") != "" {
		ak := firstNonEmpty(os.Getenv("TLS_ACCESS_KEY_ID"), os.Getenv("VOLC_ACCESS_KEY_ID"))
		sk := firstNonEmpty(os.Getenv("TLS_ACCESS_KEY_SECRET"), os.Getenv("VOLC_ACCESS_KEY_SECRET"))
		out = append(out, LogSourceConfig{
			Name:            firstNonEmpty(os.Getenv("TLS_NAME"), "volcengine-tls"),
			Type:            "volcengine_tls",
			Endpoint:        ep,
			Region:          os.Getenv("TLS_REGION"),
			TopicID:         os.Getenv("TLS_TOPIC_ID"),
			AccessKeyID:     ak,
			AccessKeySecret: sk,
			SecurityToken:   os.Getenv("TLS_SECURITY_TOKEN"),
			ServiceField:    os.Getenv("TLS_SERVICE_FIELD"),
			LevelField:      os.Getenv("TLS_LEVEL_FIELD"),
		})
	}

	return out
}

// firstNonEmpty returns the first non-empty string among its arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// DataSourceConfig holds configuration for all RCA data sources.
type DataSourceConfig struct {
	PrometheusEndpoint string
	PrometheusToken    string // optional bearer token (e.g. ARMS Prometheus)
	ARMSAPMEndpoint    string // ARMS APM Prometheus workspace (bearer-token topology)
	ARMSAPMToken       string // bearer token for the ARMS APM endpoint
	ARMSEndpoint       string
	ARMSPid            string
	ARMSAccessKey      string
	ARMSSecretKey      string
	K8sEndpoint        string
	K8sToken           string
	K8sNamespace       string
	// e-cam log federation: base URL + bearer token for the e-cam service
	// (/api/v1/cam/logs). Activates the app-log dimension across every cloud
	// account e-cam federates, without any cloud SDK credentials here.
	ECamEndpoint string
	ECamToken    string // static bearer fallback (mktok session token)
	// S2S client_credentials 配置：配置后优先用 eiam 换取携带租户的令牌，取代静态 token。
	ECamTokenURL     string
	ECamClientID     string
	ECamClientSecret string
	ECamTenantID     int64  // S2S 令牌绑定的租户（eiam 校验 allowed_tenants）
	ECamLogType      string // default "app"
	ECamTimeout      string // Go duration; default "30s"
	// LogSources lists the cloud log backends to query. Multiple entries are
	// queried concurrently and merged (multi-cloud aggregation). Empty => mock.
	LogSources []LogSourceConfig
}

// LogSourceConfig describes one cloud log backend (SLS, Volcengine TLS, ...).
// Type selects the client; the remaining fields are the union of what the
// supported clouds need (unused fields are ignored per type).
type LogSourceConfig struct {
	// Name is a human-readable identifier, attached to every log line from
	// this source so RCA can tell which cloud a line came from.
	Name string
	// Type is the data source type: "aliyun_sls" or "volcengine_tls".
	Type            string
	Endpoint        string
	Region          string // TLS only
	Project         string // SLS only
	LogStore        string // SLS only
	TopicID         string // TLS only
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	// ServiceField is the index field holding the service identifier
	// (deployment-specific; defaults to "service").
	ServiceField string
	// LevelField is the index field holding the log level, used to translate
	// an errors-only intent (deployment-specific; defaults to "level"). Access
	// logs usually lack it; point error-intent at application-log sources.
	LevelField string
}

// IsEmpty reports whether no real data source is configured.
func (c *DataSourceConfig) IsEmpty() bool {
	if c == nil {
		return true
	}
	return c.PrometheusEndpoint == "" && c.ARMSAPMEndpoint == "" &&
		c.ARMSEndpoint == "" && c.K8sEndpoint == "" &&
		c.ECamEndpoint == "" && len(c.LogSources) == 0
}

// buildLogSources constructs real cloud log clients from cfg.LogSources,
// connects each, wraps it as an intent-based RCA LogSource, and returns a
// single multi-cloud AggregateLogSource (or nil when none are configured or
// all fail to connect). A client that fails to construct or connect is skipped
// so one bad cloud never blocks startup — progressive enhancement, consistent
// with the other dimensions. (Logger is nil here; the aggregate tolerates it.)
//
// Defined as a free function, not inline in CreateRCASources, because that
// function shadows the imported `sources` package with a local variable of the
// same name — so the package is only reachable from here.
func buildLogSources(cfg *DataSourceConfig) sources.LogSource {
	if cfg == nil || (len(cfg.LogSources) == 0 && cfg.ECamEndpoint == "") {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var named []sources.NamedLogSource

	// e-cam log federation first: one source covers every cloud account the
	// e-cam service federates (app business logs with the service/level/
	// message RCA contract). Skip on ping failure like any other backend —
	// an unreachable e-cam never blocks startup.
	if cfg.ECamEndpoint != "" {
		timeout := time.Duration(0)
		if cfg.ECamTimeout != "" {
			if d, err := time.ParseDuration(cfg.ECamTimeout); err == nil {
				timeout = d
			}
		}
		ecam := sources.NewECamLogSource(cfg.ECamEndpoint, cfg.ECamToken, cfg.ECamLogType, timeout)
		if cfg.ECamTokenURL != "" {
			ecam.WithTokenProvider(sources.NewServiceTokenProvider(
				cfg.ECamTokenURL, cfg.ECamClientID, cfg.ECamClientSecret, cfg.ECamTenantID,
			))
		}
		if pingOK(ecam) {
			named = append(named, sources.NamedLogSource{Name: "e-cam-federation", Source: ecam})
		}
	}

	for _, lc := range cfg.LogSources {
		var client dscore.LogDataSource
		var err error

		switch lc.Type {
		case string(dscore.DataSourceTypeSLS):
			client, err = dslog.NewAliyunSLSClient(&dscore.AliyunSLSConfig{
				Config:          dscore.Config{Name: lc.Name, Type: dscore.DataSourceTypeSLS, Enabled: true},
				Endpoint:        lc.Endpoint,
				Project:         lc.Project,
				LogStore:        lc.LogStore,
				AccessKeyID:     lc.AccessKeyID,
				AccessKeySecret: lc.AccessKeySecret,
				SecurityToken:   lc.SecurityToken,
				ServiceField:    lc.ServiceField,
				LevelField:      lc.LevelField,
			})
		case string(dscore.DataSourceTypeTLS):
			client, err = dslog.NewVolcengineTLSClient(&dscore.VolcengineTLSConfig{
				Config:          dscore.Config{Name: lc.Name, Type: dscore.DataSourceTypeTLS, Enabled: true},
				Endpoint:        lc.Endpoint,
				Region:          lc.Region,
				TopicID:         lc.TopicID,
				AccessKeyID:     lc.AccessKeyID,
				AccessKeySecret: lc.AccessKeySecret,
				SecurityToken:   lc.SecurityToken,
				ServiceField:    lc.ServiceField,
				LevelField:      lc.LevelField,
			})
		default:
			continue
		}

		if err != nil {
			continue
		}
		if err := client.Connect(ctx); err != nil {
			continue
		}
		named = append(named, sources.NamedLogSource{
			Name:   lc.Name,
			Source: sources.NewDSLogSource(client),
		})
	}

	if len(named) == 0 {
		return nil
	}
	return sources.NewAggregateLogSource(nil, named...)
}

// pinger is implemented by real data sources that can cheaply probe their
// backend for reachability. buildLogSources already probes via Connect; this
// is the metrics/topology/changes equivalent.
type pinger interface {
	Ping(ctx context.Context) error
}

// pingOK reports whether a freshly constructed source is genuinely reachable.
// A source that does not implement pinger is assumed OK (no probe available).
// A pinger whose probe fails is NOT OK, so the caller drops it to nil and the
// progressive-enhancement fallback supplies the mock instead. This is what
// makes active["metrics"/"topology"/"changes"]=true mean "verified reachable"
// rather than merely "constructed", matching the log path's Connect semantics.
//
// 探测带 3 次退避重试(总预算 ~24s):同机 dev 编排下 e-cam/SLS 等依赖可能与
// 本进程同时重启,首探落在对端编译窗口会误判"不可达"并粘滞整个进程生命周期
// (实测 2026-10-27:opsagent 启动先于 e-cam 就绪,logs_real=false 且当日 RCA
// 日志维度全空)。
func pingOK(src interface{}) bool {
	p, ok := src.(pinger)
	if !ok {
		return true
	}
	const attempts = 3
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(6 * time.Second)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := p.Ping(ctx)
		cancel()
		if err == nil {
			return true
		}
	}
	return false
}

// CreateRCASources creates all RCA data sources from configuration. Only the
// dimensions with sufficient configuration AND a successful reachability probe
// are populated; the rest stay nil (the fallback then supplies a mock).
func CreateRCASources(cfg *DataSourceConfig) (*RCASources, error) {
	if cfg == nil {
		return nil, fmt.Errorf("data source config is required")
	}

	result := &RCASources{}
	result.LogSource = buildLogSources(cfg)

	if cfg.PrometheusEndpoint != "" {
		promSource, err := integrations.NewPrometheusMetricSourceWithToken(cfg.PrometheusEndpoint, cfg.PrometheusToken)
		if err != nil {
			return nil, fmt.Errorf("failed to create Prometheus source: %w", err)
		}
		if pingOK(promSource) {
			result.MetricSource = promSource
		}
	}

	// Topology: prefer the ARMS APM Prometheus source (bearer token only, no
	// AK/SK) when configured; otherwise fall back to the legacy ARMS OpenAPI
	// source (which requires PID + AK/SK signing).
	if cfg.ARMSAPMEndpoint != "" {
		apmSource, err := integrations.NewARMSPromTopologySource(cfg.ARMSAPMEndpoint, cfg.ARMSAPMToken)
		if err != nil {
			return nil, fmt.Errorf("failed to create ARMS APM topology source: %w", err)
		}
		if pingOK(apmSource) {
			result.TopologySource = apmSource
		}
	} else if cfg.ARMSEndpoint != "" && cfg.ARMSPid != "" {
		armsSource, err := integrations.NewARMSTopologySource(cfg.ARMSEndpoint, cfg.ARMSPid, cfg.ARMSAccessKey, cfg.ARMSSecretKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create ARMS source: %w", err)
		}
		if pingOK(armsSource) {
			result.TopologySource = armsSource
		}
	}

	if cfg.K8sEndpoint != "" && cfg.K8sToken != "" {
		k8sSource, err := integrations.NewK8sChangeSource(cfg.K8sEndpoint, cfg.K8sToken, cfg.K8sNamespace)
		if err != nil {
			return nil, fmt.Errorf("failed to create K8s source: %w", err)
		}
		if pingOK(k8sSource) {
			result.ChangeSource = k8sSource
		}
	}

	return result, nil
}

// CreateRCASourcesWithFallback builds real data sources from cfg and fills any
// unconfigured dimension with its mock equivalent. This is the progressive-
// enhancement path: the system is always functional (mock) out of the box, and
// each dimension upgrades to a real integration as soon as its credentials are
// supplied. A nil cfg yields an all-mock set.
//
// It also returns which dimensions are backed by a real source, so callers can
// report the active configuration.
func CreateRCASourcesWithFallback(cfg *DataSourceConfig) (*RCASources, map[string]bool) {
	active := map[string]bool{
		"metrics":  false,
		"topology": false,
		"changes":  false,
		"logs":     false, // upgraded to true below when a real log source connects
	}

	real := &RCASources{}
	if cfg != nil {
		if r, err := CreateRCASources(cfg); err == nil && r != nil {
			real = r
		}
	}

	// Fallback uses HONEST EMPTY sources, not the Mock* ones. The mocks
	// fabricate demo signals (most notably a hard-coded "replica count scaled
	// from 5 to 8" change pinned to always land in the incident window and
	// score high), so every analysis surfaced the same invented root cause
	// regardless of the real incident — manufacturing false confidence. An
	// unconfigured dimension must stay SILENT, producing no hypotheses, rather
	// than lie. Each empty source is overwritten below by its real integration
	// as soon as credentials exist (progressive enhancement is preserved).
	result := &RCASources{
		LogSource:      sources.NewEmptyLogSource(),
		MetricSource:   sources.NewEmptyMetricSource(),
		TraceSource:    sources.NewEmptyTraceSource(),
		TopologySource: sources.NewEmptyTopologySource(),
		ChangeSource:   sources.NewEmptyChangeSource(),
	}
	if real.LogSource != nil {
		result.LogSource = real.LogSource
		active["logs"] = true
	}
	if real.MetricSource != nil {
		result.MetricSource = real.MetricSource
		active["metrics"] = true
	}
	if real.TopologySource != nil {
		result.TopologySource = real.TopologySource
		active["topology"] = true
	}
	if real.ChangeSource != nil {
		result.ChangeSource = real.ChangeSource
		active["changes"] = true
	}

	return result, active
}

// CreateMockRCASources creates mock data sources for testing.
func CreateMockRCASources() *RCASources {
	return &RCASources{
		LogSource:      &MockLogSource{},
		MetricSource:   &MockMetricSource{},
		TopologySource: &MockTopologySource{},
		ChangeSource:   &MockChangeSource{},
	}
}

// MockMetricSource implements sources.MetricSource for testing.
type MockMetricSource struct{}

func (m *MockMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	return []models.MetricSeries{
		{
			Name:   "http_requests_total",
			Labels: map[string]string{"service": "order-service", "status": "5xx"},
			Unit:   "requests",
			DataPoints: []models.DataPoint{
				{Timestamp: start, Value: 0.01},
				{Timestamp: start.Add(5 * time.Minute), Value: 0.25},
			},
		},
	}, nil
}

func (m *MockMetricSource) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	return &models.MetricSeries{
		Name:   metricName,
		Labels: filters,
		Unit:   "value",
		DataPoints: []models.DataPoint{
			{Timestamp: start, Value: 100},
			{Timestamp: end, Value: 200},
		},
	}, nil
}

func (m *MockMetricSource) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	return []models.MetricSeries{
		{
			Name:   "http_requests_total",
			Labels: map[string]string{"service": service},
			Unit:   "requests",
			DataPoints: []models.DataPoint{
				{Timestamp: start, Value: 50},
			},
		},
	}, nil
}

func (m *MockMetricSource) GetAlerts(ctx context.Context) ([]sources.Alert, error) {
	return []sources.Alert{
		{
			Labels:      map[string]string{"alert": "HighErrorRate", "service": "order-service"},
			Annotations: map[string]string{"summary": "High error rate detected"},
			State:       "firing",
		},
	}, nil
}

// MockTopologySource implements sources.TopologySource for testing.
type MockTopologySource struct{}

func (m *MockTopologySource) GetTopology(ctx context.Context) (*models.Topology, error) {
	return &models.Topology{
		Nodes: []models.ServiceNode{
			{Name: "order-service", Type: "application"},
			{Name: "payment-service", Type: "application"},
		},
		Edges: []models.DependencyEdge{
			{From: "order-service", To: "payment-service", Type: "http"},
		},
	}, nil
}

func (m *MockTopologySource) GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error) {
	if service == "order-service" {
		return nil, []string{"payment-service"}, nil
	}
	return []string{"order-service"}, nil, nil
}

func (m *MockTopologySource) GetServiceHealth(ctx context.Context) (map[string]string, error) {
	return map[string]string{
		"order-service":   "healthy",
		"payment-service": "healthy",
	}, nil
}

// MockChangeSource implements sources.ChangeSource for testing.
type MockChangeSource struct{}

func (m *MockChangeSource) QueryChanges(ctx context.Context, start, end time.Time) ([]models.Change, error) {
	return []models.Change{
		{
			ID:        "deploy-001",
			Timestamp: start.Add(30 * time.Minute),
			Service:   "order-service",
			Type:      "deployment",
		},
	}, nil
}

func (m *MockChangeSource) QueryChangesByService(ctx context.Context, service string, start, end time.Time) ([]models.Change, error) {
	return []models.Change{
		{
			ID:        "deploy-001",
			Timestamp: start.Add(30 * time.Minute),
			Service:   service,
			Type:      "deployment",
		},
	}, nil
}

func (m *MockChangeSource) GetLastChange(ctx context.Context, service string) (*models.Change, error) {
	return &models.Change{
		ID:        "deploy-001",
		Service:   service,
		Timestamp: time.Now(),
		Type:      "deployment",
	}, nil
}

// MockLogSource implements sources.LogSource for testing.
type MockLogSource struct{}

func (m *MockLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	return []models.LogEntry{
		{
			Timestamp: start.Add(10 * time.Minute),
			Service:   "order-service",
			Level:     "error",
			Message:   "Connection refused to database at 192.168.1.100",
		},
		{
			Timestamp: start.Add(15 * time.Minute),
			Service:   "order-service",
			Level:     "error",
			Message:   "Connection refused to database at 192.168.1.101",
		},
		{
			Timestamp: start.Add(20 * time.Minute),
			Service:   "order-service",
			Level:     "error",
			Message:   "Timeout waiting for response from api-gateway",
		},
		{
			Timestamp: start.Add(25 * time.Minute),
			Service:   "order-service",
			Level:     "error",
			Message:   "Timeout waiting for response from api-gateway",
		},
	}, nil
}

func (m *MockLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	return []models.LogEntry{
		{
			Timestamp: start.Add(5 * time.Minute),
			Service:   service,
			Level:     "info",
			Message:   "Request received",
		},
		{
			Timestamp: start.Add(10 * time.Minute),
			Service:   service,
			Level:     "error",
			Message:   "Connection refused",
		},
	}, nil
}

func (m *MockLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	// Return different counts based on service
	if service == "order-service" {
		return 100, nil
	}
	return 50, nil
}
