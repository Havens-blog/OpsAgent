package interfaces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// TestWAFToolDefaultsRecentWindow guards the time-window contract: when the
// model omits start_time/end_time, the tool must default to the last 24h rather
// than querying a zero/ancient window and reporting a misleading "no attacks".
func TestWAFToolDefaultsRecentWindow(t *testing.T) {
	var gotStart, gotEnd int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			StartTime int64 `json:"start_time"`
			EndTime   int64 `json:"end_time"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotStart, gotEnd = body.StartTime, body.EndTime
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"total":0,"entries":[]}}`))
	}))
	defer srv.Close()

	src := sources.NewECamWAFSource(srv.URL, "", 5*time.Second)
	q := &wafQuerier{src: src}
	tool := createWAFLogQueryTool(q)
	if tool == nil {
		t.Fatal("expected tool")
	}

	before := time.Now()
	if _, err := tool.Run(context.Background(), map[string]interface{}{"domain": "www.jlc-smt.com"}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	after := time.Now()

	if gotEnd < before.Add(-2*time.Second).UnixMilli() || gotEnd > after.Add(2*time.Second).UnixMilli() {
		t.Fatalf("end_time = %d, want ~now (%d..%d)", gotEnd, before.UnixMilli(), after.UnixMilli())
	}
	wantStart := before.Add(-24 * time.Hour).UnixMilli()
	if d := gotStart - wantStart; d < -2000 || d > 2000 {
		t.Fatalf("start_time = %d, want ~now-24h (%d)", gotStart, wantStart)
	}
}
