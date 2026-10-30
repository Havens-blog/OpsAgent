// Package models provides tests for RCA data models.
package models

import (
	"strings"
	"testing"
	"time"
)

// TestWAFFactsHonesty locks the two facts on-call decisions hinge on: a capped
// sample must not be reported as a total, and an alarm's "blocked count" must
// not hide that observation-mode rules let the requests through.
func TestWAFFactsHonesty(t *testing.T) {
	window := time.Date(2026, 10, 28, 16, 8, 0, 0, time.Local)

	t.Run("capped sample reports at-least, never a total", func(t *testing.T) {
		f := &WAFAttackFacts{Domain: "www.jlc-smt.com", Total: 500, SampleCapped: true,
			WindowStart: window, WindowEnd: window.Add(time.Hour)}
		got := f.CountPhrase()
		if !strings.Contains(got, "≥500") || !strings.Contains(got, "采样上限") {
			t.Errorf("capped sample must read as a lower bound, got %q", got)
		}
	})

	t.Run("federation match count outranks the sample", func(t *testing.T) {
		f := &WAFAttackFacts{Domain: "www.jlc-smt.com", Total: 500, Matched: 12345, SampleCapped: true}
		got := f.CountPhrase()
		if !strings.Contains(got, "12345") || !strings.Contains(got, "采样 500") {
			t.Errorf("matched count must lead with sampling noted, got %q", got)
		}
	})

	t.Run("observation-mode majority is called out", func(t *testing.T) {
		// Real FB982W shape: 247 alert + 244 js challenge, only 8 real blocks.
		f := &WAFAttackFacts{Domain: "www.jlc-smt.com", Total: 499, Actions: []FactCount{
			{Key: "alert", Count: 247}, {Key: "js", Count: 244}, {Key: "block", Count: 8},
		}}
		got := f.Verdict()
		if !strings.Contains(got, "实际拦截仅 8 条") || !strings.Contains(got, "观察 247(不阻断)") {
			t.Errorf("verdict must expose the real block count and note observation mode, got %q", got)
		}
		if actions := JoinActionCounts(f.Actions, 4); !strings.Contains(actions, "观察(未拦截)") {
			t.Errorf("alert must read as not-blocked, got %q", actions)
		}
	})

	t.Run("pass entries escalate to manual review", func(t *testing.T) {
		f := &WAFAttackFacts{Total: 10, Actions: []FactCount{{Key: "pass", Count: 3}, {Key: "block", Count: 7}}}
		if got := f.Verdict(); !strings.Contains(got, "放行") || !strings.Contains(got, "复核") {
			t.Errorf("pass entries must demand review, got %q", got)
		}
	})
}

// TestExtractWAFDomain pins exact-domain extraction: WatchAlert suffixes the
// domain with "-waf", and subdomains are distinct protected objects.
func TestExtractWAFDomain(t *testing.T) {
	tests := []struct{ text, want string }{
		{"阿里云WAF：域名 www.jlc-smt.com-waf 最近30分钟CC攻击拦截量峰值超过1000次", "www.jlc-smt.com"},
		{"域名: proxy.jlc.com-access 异常", "proxy.jlc.com"},
		{"域名：api.jlc.com 攻击", "api.jlc.com"},
		{"拨测任务：https://www.jlc-smt.com/144984416 可用率88.89% 不可用1次", "www.jlc-smt.com"},
		{"Pod Unhealthy in prod-finance", ""},
	}
	for _, tt := range tests {
		if got := ExtractWAFDomain(tt.text); got != tt.want {
			t.Errorf("ExtractWAFDomain(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

// TestIsWAFAlarm pins the distinction that keeps synthetic-probe alarms off
// the WAF dimension: a dial-test probe is WAF-whitelisted and can never be
// blocked, so only genuinely WAF-shaped alarms may anchor on WAF facts.
func TestIsWAFAlarm(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"阿里云WAF：域名 www.jlc-smt.com-waf 最近30分钟CC攻击拦截量峰值超过1000次", true},
		{"WAF-Web攻击拦截突增", true},
		{"您的云拨测产生[普通告警] 拨测任务：https://www.jlc-smt.com/144984416 可用率88.89%", false},
		{"服务 xxx 错误率突增", false},
	}
	for _, tt := range tests {
		if got := IsWAFAlarm(tt.text); got != tt.want {
			t.Errorf("IsWAFAlarm(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

// TestExtractBusinessUnitToken pins the domain->BU anchor that keeps a
// domain-scoped alarm focused on its own backend (jlc-smt.com -> smt).
func TestExtractBusinessUnitToken(t *testing.T) {
	tests := []struct{ text, want string }{
		{"拨测任务：https://www.jlc-smt.com/144984416 可用率88.89%", "smt"},
		{"拨测任务：https://api.jlc-pcb.com/x 不可用", "pcb"},
		{"Pod Unhealthy in prod-finance", ""},
	}
	for _, tt := range tests {
		if got := ExtractBusinessUnitToken(tt.text); got != tt.want {
			t.Errorf("ExtractBusinessUnitToken(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

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
