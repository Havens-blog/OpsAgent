package log

import (
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
)

// slsClientForTest builds an SLS client shell sufficient to exercise
// buildQuery (which only reads config + request, never the SDK handle).
func slsClientForTest(serviceField string) *AliyunSLSClient {
	return &AliyunSLSClient{
		config: &AliyunSLSConfig{AliyunSLSConfig: &core.AliyunSLSConfig{
			ServiceField: serviceField,
		}},
	}
}

func tlsClientForTest(serviceField string) *VolcengineTLSClient {
	return &VolcengineTLSClient{
		config: &core.VolcengineTLSConfig{ServiceField: serviceField},
	}
}

func TestSLSBuildQuery(t *testing.T) {
	tests := []struct {
		name         string
		serviceField string
		req          *core.LogQueryRequest
		want         string
	}{
		{
			name: "nil intent uses raw query verbatim",
			req:  &core.LogQueryRequest{Query: "status: 500"},
			want: "status: 500",
		},
		{
			name: "errors-only intent translates to SLS level dialect",
			req:  &core.LogQueryRequest{Intent: &core.QueryIntent{ErrorsOnly: true}},
			want: "(level: ERROR OR level: ERR)",
		},
		{
			name: "service intent uses default service field",
			req:  &core.LogQueryRequest{Intent: &core.QueryIntent{ServiceName: "order"}},
			want: "service: order",
		},
		{
			name:         "service intent uses configured field",
			serviceField: "app_name",
			req:          &core.LogQueryRequest{Intent: &core.QueryIntent{ServiceName: "order"}},
			want:         "app_name: order",
		},
		{
			name: "errors + service are AND-combined",
			req:  &core.LogQueryRequest{Intent: &core.QueryIntent{ErrorsOnly: true, ServiceName: "pay"}},
			want: "(level: ERROR OR level: ERR) AND service: pay",
		},
		{
			name: "raw query is AND-combined with intent as extra constraint",
			req:  &core.LogQueryRequest{Intent: &core.QueryIntent{ErrorsOnly: true}, Query: "host: h1"},
			want: "(level: ERROR OR level: ERR) AND (host: h1)",
		},
		{
			name: "empty intent matches everything",
			req:  &core.LogQueryRequest{Intent: &core.QueryIntent{}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := slsClientForTest(tt.serviceField)
			if got := c.buildQuery(tt.req); got != tt.want {
				t.Errorf("buildQuery() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTLSBuildQuery(t *testing.T) {
	tests := []struct {
		name         string
		serviceField string
		req          *core.LogQueryRequest
		want         string
	}{
		{
			name: "nil intent uses raw query verbatim",
			req:  &core.LogQueryRequest{Query: "status:500"},
			want: "status:500",
		},
		{
			name: "errors-only intent translates to TLS level dialect",
			req:  &core.LogQueryRequest{Intent: &core.QueryIntent{ErrorsOnly: true}},
			want: "(level:ERROR OR level:ERR)",
		},
		{
			name: "service intent uses default service field",
			req:  &core.LogQueryRequest{Intent: &core.QueryIntent{ServiceName: "order"}},
			want: "service:order",
		},
		{
			name:         "service intent uses configured field",
			serviceField: "svc_name",
			req:          &core.LogQueryRequest{Intent: &core.QueryIntent{ServiceName: "order"}},
			want:         "svc_name:order",
		},
		{
			name: "errors + service are AND-combined",
			req:  &core.LogQueryRequest{Intent: &core.QueryIntent{ErrorsOnly: true, ServiceName: "pay"}},
			want: "(level:ERROR OR level:ERR) AND service:pay",
		},
		{
			name: "empty intent matches everything",
			req:  &core.LogQueryRequest{Intent: &core.QueryIntent{}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tlsClientForTest(tt.serviceField)
			if got := c.buildQuery(tt.req); got != tt.want {
				t.Errorf("buildQuery() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSLSLevelFieldConfigurable verifies the log-level field used to translate
// an ErrorsOnly intent is configurable (defaulting to "level"). This exists
// because the hardcoded "level" field made error-intent queries fail against
// real logstores that index the level under a different name (or not at all,
// as with access logs).
func TestSLSLevelFieldConfigurable(t *testing.T) {
	errReq := &core.LogQueryRequest{Intent: &core.QueryIntent{ErrorsOnly: true}}

	// Default when unset.
	def := &AliyunSLSClient{config: &AliyunSLSConfig{AliyunSLSConfig: &core.AliyunSLSConfig{}}}
	if got, want := def.buildQuery(errReq), "(level: ERROR OR level: ERR)"; got != want {
		t.Errorf("default level field: got %q, want %q", got, want)
	}

	// Custom level field name.
	custom := &AliyunSLSClient{config: &AliyunSLSConfig{AliyunSLSConfig: &core.AliyunSLSConfig{LevelField: "severity"}}}
	if got, want := custom.buildQuery(errReq), "(severity: ERROR OR severity: ERR)"; got != want {
		t.Errorf("custom level field: got %q, want %q", got, want)
	}
}

func TestTLSLevelFieldConfigurable(t *testing.T) {
	errReq := &core.LogQueryRequest{Intent: &core.QueryIntent{ErrorsOnly: true}}

	def := &VolcengineTLSClient{config: &core.VolcengineTLSConfig{}}
	if got, want := def.buildQuery(errReq), "(level:ERROR OR level:ERR)"; got != want {
		t.Errorf("default level field: got %q, want %q", got, want)
	}

	custom := &VolcengineTLSClient{config: &core.VolcengineTLSConfig{LevelField: "severity"}}
	if got, want := custom.buildQuery(errReq), "(severity:ERROR OR severity:ERR)"; got != want {
		t.Errorf("custom level field: got %q, want %q", got, want)
	}
}

// TestBackfillSource locks in the service-attribution fix: when ParseLogEntry
// could not populate Source from a conventional "source" key, the configured
// service field resolves it from Fields. This is what let 196 real Volcengine
// TLS error logs (whose service lived in "document_type") reach RCA with a
// service name instead of collapsing into one empty-service bucket.
func TestBackfillSource(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		fields       map[string]interface{}
		serviceField string
		want         string
	}{
		{
			name:         "resolves service from configured field",
			fields:       map[string]interface{}{"document_type": "fa-followup-system-business"},
			serviceField: "document_type",
			want:         "fa-followup-system-business",
		},
		{
			name:         "does not overwrite an already-set source",
			source:       "already-known",
			fields:       map[string]interface{}{"document_type": "other"},
			serviceField: "document_type",
			want:         "already-known",
		},
		{
			name:         "no-op when serviceField unset (no regression)",
			fields:       map[string]interface{}{"document_type": "svc"},
			serviceField: "",
			want:         "",
		},
		{
			name:         "no-op when configured field absent",
			fields:       map[string]interface{}{"other": "x"},
			serviceField: "document_type",
			want:         "",
		},
		{
			name:         "no-op when field value is not a string",
			fields:       map[string]interface{}{"document_type": 123},
			serviceField: "document_type",
			want:         "",
		},
		{
			name:         "no-op when field value is empty string",
			fields:       map[string]interface{}{"document_type": ""},
			serviceField: "document_type",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &core.LogEntry{Source: tt.source, Fields: tt.fields}
			backfillSource(entry, tt.serviceField)
			if entry.Source != tt.want {
				t.Errorf("backfillSource() Source = %q, want %q", entry.Source, tt.want)
			}
		})
	}

	// nil entry must not panic.
	backfillSource(nil, "document_type")
}
