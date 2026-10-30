// Package sources provides data source interfaces for RCA analysis.
package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newECamStub spins up an httptest server speaking the e-cam federation
// envelope and returns the source pointed at it plus a request recorder.
func newECamStub(t *testing.T, handler http.HandlerFunc) (*ECamLogSource, *[]recordedRequest) {
	t.Helper()
	var got []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recordedRequest{Path: r.URL.Path, Auth: r.Header.Get("Authorization")}
		if r.Method == http.MethodPost {
			dec := json.NewDecoder(r.Body)
			_ = dec.Decode(&rec.Body)
		}
		got = append(got, rec)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return NewECamLogSource(srv.URL, "test-token", "", 5*time.Second), &got
}

type recordedRequest struct {
	Path string
	Auth string
	Body map[string]any
}

// envelope writes a standard e-cam response.
func envelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func TestECamQueryErrorsContract(t *testing.T) {
	src, reqs := newECamStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/cam/logs/search" {
			envelope(w, map[string]any{
				"total": 2,
				"entries": []map[string]any{
					{
						"timestamp": 1793015511000,
						"service":   "cnc-third-order-system",
						"level":     "ERROR",
						"message":   "---请求失败--customize=",
						"exception": "BusinessException: x",
						"trace_id":  "abc123",
						"meta": map[string]any{
							"cloud": "aliyun", "region": "cn-shenzhen",
							"source": "jlc-prod-log/cnc-third-order-system-business",
						},
					},
					{
						"timestamp": 1793015512000,
						"service":   "core-access-gateway",
						"level":     "error",
						"message":   "upstream timeout",
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	start := time.Date(2026, 10, 26, 19, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	entries, err := src.QueryErrors(context.Background(), start, end)
	if err != nil {
		t.Fatalf("QueryErrors: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	// 请求契约:log_type=app、毫秒窗口、level=error 过滤、Bearer 认证
	if len(*reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(*reqs))
	}
	req := (*reqs)[0]
	if req.Path != "/api/v1/cam/logs/search" {
		t.Errorf("path = %s", req.Path)
	}
	if req.Auth != "Bearer test-token" {
		t.Errorf("auth = %q, want bearer token", req.Auth)
	}
	if req.Body["log_type"] != "app" {
		t.Errorf("log_type = %v, want app", req.Body["log_type"])
	}
	if req.Body["start_time"] != float64(start.UnixMilli()) {
		t.Errorf("start_time = %v, want %d (Unix ms)", req.Body["start_time"], start.UnixMilli())
	}
	filters, _ := req.Body["filters"].([]any)
	if len(filters) != 1 {
		t.Fatalf("filters = %v, want single level=error", req.Body["filters"])
	}
	f := filters[0].(map[string]any)
	if f["field"] != "level" || f["op"] != "eq" || f["value"] != "error" {
		t.Errorf("filter = %v, want level eq error", f)
	}

	// 映射:异常/trace/来源透传,IsError 判定,级别归一小写
	e := entries[0]
	if e.Service != "cnc-third-order-system" || e.Level != "error" || !e.IsError {
		t.Errorf("entry0 = %+v", e)
	}
	if e.TraceID != "abc123" {
		t.Errorf("TraceID = %q", e.TraceID)
	}
	if e.Labels["exception"] != "BusinessException: x" {
		t.Errorf("Labels[exception] = %q", e.Labels["exception"])
	}
	if e.Labels["source"] != "jlc-prod-log/cnc-third-order-system-business" {
		t.Errorf("Labels[source] = %q", e.Labels["source"])
	}
	if !e.Timestamp.Equal(time.UnixMilli(1793015511000)) {
		t.Errorf("Timestamp = %v", e.Timestamp)
	}
	if entries[1].Labels["exception"] != "" || entries[1].IsError == false {
		t.Errorf("entry1 mapping = %+v", entries[1])
	}
}

func TestECamQueryLogsByServiceContract(t *testing.T) {
	src, reqs := newECamStub(t, func(w http.ResponseWriter, r *http.Request) {
		envelope(w, map[string]any{"total": 0, "entries": []any{}})
	})
	start := time.Now().Add(-time.Hour)
	entries, err := src.QueryLogsByService(context.Background(), "cnc-sorter-center", start, time.Now())
	if err != nil {
		t.Fatalf("QueryLogsByService: %v", err)
	}
	if entries == nil || len(entries) != 0 {
		t.Fatalf("空结果应返回空切片(诚实空), got %v", entries)
	}
	// store 级收敛:app 日志走 resources=[<service>-business](单店直查),
	// 不再下 service 字段过滤(那会扇出全部 ~800 店)
	res, _ := (*reqs)[0].Body["resources"].([]any)
	if len(res) != 1 || res[0] != "cnc-sorter-center-business" {
		t.Errorf("resources = %v, want [cnc-sorter-center-business]", (*reqs)[0].Body["resources"])
	}
	if filters, ok := (*reqs)[0].Body["filters"].([]any); ok && len(filters) != 0 {
		t.Errorf("app 类型不应携带 service 过滤, got %v", filters)
	}
}

func TestECamQueryErrorsByServiceContract(t *testing.T) {
	src, reqs := newECamStub(t, func(w http.ResponseWriter, r *http.Request) {
		envelope(w, map[string]any{"total": 0, "entries": []any{}})
	})
	start := time.Now().Add(-time.Hour)
	if _, err := src.QueryErrorsByService(context.Background(), "cnc-sorter-center", start, time.Now()); err != nil {
		t.Fatalf("QueryErrorsByService: %v", err)
	}
	// 引擎按事发服务收敛日志的主路径:store 圈定 + level=error 下推
	res, _ := (*reqs)[0].Body["resources"].([]any)
	if len(res) != 1 || res[0] != "cnc-sorter-center-business" {
		t.Errorf("resources = %v, want [cnc-sorter-center-business]", (*reqs)[0].Body["resources"])
	}
	filters, _ := (*reqs)[0].Body["filters"].([]any)
	if len(filters) != 1 {
		t.Fatalf("filters = %v, want single level=error", (*reqs)[0].Body["filters"])
	}
	f := filters[0].(map[string]any)
	if f["field"] != "level" || f["op"] != "eq" || f["value"] != "error" {
		t.Errorf("filter = %v, want level eq error", f)
	}
	// 空服务名必须报错,不许静默全量
	if _, err := src.QueryErrorsByService(context.Background(), " ", start, time.Now()); err == nil {
		t.Error("空服务名应报错")
	}
}

func TestECamGetLogCountAggregateThenFallback(t *testing.T) {
	// 1) /aggregate 可用:精确计数直返
	src, reqs := newECamStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/cam/logs/aggregate":
			envelope(w, map[string]any{"total": 20704, "buckets": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	start := time.Now().Add(-time.Hour)
	count, err := src.GetLogCount(context.Background(), "cnc-third-order-system", start, time.Now())
	if err != nil {
		t.Fatalf("GetLogCount: %v", err)
	}
	if count != 20704 {
		t.Fatalf("count = %d, want 20704", count)
	}
	req := (*reqs)[0]
	if req.Path != "/api/v1/cam/logs/aggregate" {
		t.Fatalf("path = %s, want aggregate first", req.Path)
	}
	if req.Body["metric"] != "count" {
		t.Errorf("metric = %v, want count", req.Body["metric"])
	}

	// 2) /aggregate 失败:回退搜索计数(有界下界,不编造精确值)
	src2, reqs2 := newECamStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/cam/logs/aggregate":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":40301,"message":"denied"}`))
		case "/api/v1/cam/logs/search":
			envelope(w, map[string]any{"total": 300, "entries": []any{
				map[string]any{"timestamp": 1, "service": "s", "level": "info", "message": "x"},
				map[string]any{"timestamp": 2, "service": "s", "level": "info", "message": "y"},
			}})
		}
	})
	count2, err := src2.GetLogCount(context.Background(), "s", start, time.Now())
	if err != nil {
		t.Fatalf("GetLogCount fallback: %v", err)
	}
	if count2 != 2 {
		t.Fatalf("fallback count = %d, want 2(返回条数,不编造)", count2)
	}
	if len(*reqs2) != 2 {
		t.Fatalf("requests = %d, want aggregate+search", len(*reqs2))
	}
}

func TestECamErrorsAreErrors(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	end := time.Now()

	// 非 200:透出错误,绝不返回假数据
	src, _ := newECamStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":40101,"message":"unauthorized"}`))
	})
	if _, err := src.QueryErrors(context.Background(), start, end); err == nil {
		t.Fatalf("401 应返回错误")
	}

	// code != 0:透出错误
	src2, _ := newECamStub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":50000,"message":"internal"}`))
	})
	if _, err := src2.QueryLogsByService(context.Background(), "s", start, end); err == nil {
		t.Fatalf("code!=0 应返回错误")
	}

	// 传输失败:透出错误
	broken := NewECamLogSource("http://127.0.0.1:1", "t", "", time.Second)
	if _, err := broken.QueryErrors(context.Background(), start, end); err == nil {
		t.Fatalf("连接失败应返回错误")
	}

	// 空 service 名:拒绝(不打无意义请求)
	if _, err := src.QueryLogsByService(context.Background(), " ", start, end); err == nil {
		t.Fatalf("空 service 名应返回错误")
	}
}

func TestECamPing(t *testing.T) {
	src, _ := newECamStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/cam/logs/types" && r.Method == http.MethodGet {
			envelope(w, []any{map[string]any{"type": "app"}})
			return
		}
		http.NotFound(w, r)
	})
	if err := src.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	down := NewECamLogSource("http://127.0.0.1:1", "t", "", time.Second)
	if err := down.Ping(context.Background()); err == nil {
		t.Fatalf("不可达 Ping 应失败")
	}
}

func TestECamEndpointTrimmedAndDefaults(t *testing.T) {
	src := NewECamLogSource("http://x:8001/", "tok", "", 0)
	if !strings.HasSuffix(src.endpoint, "/x:8001") || strings.HasSuffix(src.endpoint, "/") {
		t.Errorf("endpoint 尾斜杠应去掉, got %q", src.endpoint)
	}
	if src.logType != "app" {
		t.Errorf("默认 logType = %q, want app", src.logType)
	}
	if src.client.Timeout != 30*time.Second {
		t.Errorf("默认超时 = %v, want 30s", src.client.Timeout)
	}
	custom := NewECamLogSource("http://x", "t", "slb", 7*time.Second)
	if custom.logType != "slb" || custom.client.Timeout != 7*time.Second {
		t.Errorf("自定义参数未生效: %q %v", custom.logType, custom.client.Timeout)
	}
}
