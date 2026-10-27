package sources_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dscore "github.com/Havens-blog/OpsAgent/internal/datasource/core"
	dslog "github.com/Havens-blog/OpsAgent/internal/datasource/log"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// TestLogStackEndToEnd exercises the EXACT stack RCA uses — a real Volcengine
// TLS SDK client (issuing real HTTP), wrapped as an intent-based DSLogSource,
// placed behind the multi-cloud AggregateLogSource, and queried through the
// RCA LogSource interface. It is driven against a local server standing in for
// the TLS endpoint, so it proves the whole chain is genuinely wired end to end
// (everything but live cloud auth). This is the strongest "logs are really
// usable from RCA" check short of hitting a real cloud.
func TestLogStackEndToEnd(t *testing.T) {
	var sawErrorsQuery bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/DescribeTopic":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"TopicId":"t1","TopicName":"demo"}`)
		case "/SearchLogs":
			b, _ := io.ReadAll(r.Body)
			if strings.Contains(string(b), "level:ERROR") {
				sawErrorsQuery = true
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
				"ResultStatus":"OK","ListOver":true,"HitCount":2,"Count":2,
				"Logs":[
					{"__time__":1700000300,"level":"ERROR","message":"boom","service":"pay"},
					{"__time__":1700000100,"level":"ERROR","message":"fail","service":"pay"}
				]
			}`)
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Build the REAL TLS client against the fake endpoint — same constructor
	// path buildLogSources uses in production.
	client, err := dslog.NewVolcengineTLSClient(&dscore.VolcengineTLSConfig{
		Config:          dscore.Config{Name: "volc-e2e", Type: dscore.DataSourceTypeTLS, Enabled: true},
		Endpoint:        srv.URL,
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
		t.Fatalf("Connect: %v", err)
	}

	// Wrap exactly as production does: DSLogSource -> AggregateLogSource.
	agg := sources.NewAggregateLogSource(nil, sources.NamedLogSource{
		Name:   "volc-e2e",
		Source: sources.NewDSLogSource(client),
	})

	// Query through the RCA LogSource interface.
	var ls sources.LogSource = agg
	entries, err := ls.QueryErrors(ctx, time.Unix(1700000000, 0), time.Unix(1700001000, 0))
	if err != nil {
		t.Fatalf("QueryErrors through full stack: %v", err)
	}

	// The errors-only intent must have been translated and sent on the wire.
	if !sawErrorsQuery {
		t.Error("errors-only intent did not reach the wire as a level query")
	}

	// Entries flowed back through DSLogSource conversion + aggregate merge.
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries through the stack, got %d", len(entries))
	}

	// Aggregate tagged each entry with its origin cloud.
	for _, e := range entries {
		if e.Labels[sources.SourceLabelKey] != "volc-e2e" {
			t.Errorf("entry missing/incorrect cloud_source label: %q", e.Labels[sources.SourceLabelKey])
		}
	}

	// Newest-first ordering from the aggregate merge.
	if entries[0].Timestamp.Unix() != 1700000300 {
		t.Errorf("expected newest entry first (1700000300), got %d", entries[0].Timestamp.Unix())
	}

	// GetLogCount flows through too.
	n, err := ls.GetLogCount(ctx, "pay", time.Unix(1700000000, 0), time.Unix(1700001000, 0))
	if err != nil {
		t.Fatalf("GetLogCount through full stack: %v", err)
	}
	if n != 2 {
		t.Errorf("GetLogCount = %d, want 2", n)
	}
}
