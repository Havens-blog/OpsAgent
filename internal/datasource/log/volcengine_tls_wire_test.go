package log

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
)

// TestVolcengineTLSWireRealRequest drives the REAL Volcengine TLS SDK against a
// local HTTP server standing in for the TLS endpoint. This proves the log path
// is genuinely wired — the SDK really issues an HTTP request, the translated
// intent travels on the wire, and a real JSON response is parsed back — rather
// than only exercising pure translation logic as the unit tests do. (The one
// thing it cannot cover is real cloud auth, which needs live credentials.)
func TestVolcengineTLSWireRealRequest(t *testing.T) {
	var capturedSearchBody string
	var searchHit bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/DescribeTopic":
			// Connect() probes the topic; return a minimal valid 200 JSON.
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"TopicId":"t1","TopicName":"demo"}`)
		case "/SearchLogs":
			searchHit = true
			b, _ := io.ReadAll(r.Body)
			capturedSearchBody = string(b)
			w.Header().Set("Content-Type", "application/json")
			// Note: NO Content-Encoding: gz header, so the SDK reads plain JSON.
			// Numbers here are decoded under decoder.UseNumber() by the SDK, so
			// map values arrive as json.Number — the convert path must handle it.
			_, _ = io.WriteString(w, `{
				"ResultStatus":"OK",
				"ListOver":true,
				"HitCount":2,
				"Count":2,
				"Logs":[
					{"__time__":1700000300,"level":"ERROR","message":"boom","service":"pay"},
					{"__time__":1700000100,"level":"ERROR","message":"fail","service":"pay"}
				]
			}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Build the REAL client against the fake endpoint. Region + dummy AK/SK are
	// required by the SDK; the fake server does not verify the signature.
	client, err := NewVolcengineTLSClient(&core.VolcengineTLSConfig{
		Config:          core.Config{Name: "volc-wire", Type: core.DataSourceTypeTLS, Enabled: true},
		Endpoint:        srv.URL, // http://127.0.0.1:PORT — SDK concatenates verbatim
		Region:          "cn-test",
		TopicID:         "t1",
		AccessKeyID:     "dummy-ak",
		AccessKeySecret: "dummy-sk",
		ServiceField:    "service",
	})
	if err != nil {
		t.Fatalf("NewVolcengineTLSClient: %v", err)
	}

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect (real HTTP to fake /DescribeTopic): %v", err)
	}

	resp, err := client.QueryLogs(ctx, &core.LogQueryRequest{
		StartTime: time.Unix(1700000000, 0),
		EndTime:   time.Unix(1700001000, 0),
		Intent:    &core.QueryIntent{ErrorsOnly: true, ServiceName: "pay"},
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("QueryLogs over the wire: %v", err)
	}

	// 1) The SDK really issued the SearchLogs HTTP call.
	if !searchHit {
		t.Fatal("SearchLogs endpoint was never hit — the call did not go over the wire")
	}

	// 2) The translated INTENT traveled on the wire (not a raw/empty query).
	var sent struct {
		Query   string `json:"Query"`
		TopicID string `json:"TopicId"`
	}
	if err := json.Unmarshal([]byte(capturedSearchBody), &sent); err != nil {
		t.Fatalf("captured body is not valid JSON: %v\nbody=%s", err, capturedSearchBody)
	}
	if !strings.Contains(sent.Query, "level:ERROR") || !strings.Contains(sent.Query, "service:pay") {
		t.Errorf("wire query missing translated intent; got %q", sent.Query)
	}
	if sent.TopicID != "t1" {
		t.Errorf("wire TopicId = %q, want t1", sent.TopicID)
	}

	// 3) The real JSON response parsed back into entries.
	if len(resp.Logs) != 2 {
		t.Fatalf("expected 2 parsed log entries, got %d", len(resp.Logs))
	}
	if resp.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2 (from HitCount)", resp.TotalCount)
	}
	if resp.Logs[0].Message == "" {
		t.Error("log message not parsed from wire response")
	}

	// 4) The __time__ field parsed into a real timestamp (guards the
	// json.Number interaction from decoder.UseNumber()).
	foundTime := false
	for _, e := range resp.Logs {
		if e.Timestamp.Unix() == 1700000300 || e.Timestamp.Unix() == 1700000100 {
			foundTime = true
			break
		}
	}
	if !foundTime {
		t.Errorf("__time__ not parsed into entry timestamp; got %v / %v",
			resp.Logs[0].Timestamp.Unix(), resp.Logs[1].Timestamp.Unix())
	}
}
