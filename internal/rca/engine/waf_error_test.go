package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

type mockWAFSource struct {
	facts *models.WAFAttackFacts
	err   error
}

func (m *mockWAFSource) QueryAttackFacts(_ context.Context, _ string, _, _ time.Time) (*models.WAFAttackFacts, error) {
	return m.facts, m.err
}

// TestWAFFailureSurfacesWarning guards that a WAF query failure is recorded in
// the report warnings instead of silently dropping the WAF dimension and
// falling back to an unrelated metric hypothesis.
func TestWAFFailureSurfacesWarning(t *testing.T) {
	eng, err := NewEngine(
		WithLogSource(&mockLogSource{}),
		WithMetricSource(&mockMetricSource{}),
		WithWAFSource(&mockWAFSource{err: errors.New("waf upstream down")}),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	now := time.Now()
	incident := &models.Incident{
		ID:          "inc-waf-1",
		Severity:    "P2",
		Description: "阿里云WAF：域名 www.jlc-smt.com-waf 最近30分钟CC攻击拦截量峰值超过1000次，疑似CC攻击进行中",
		StartTime:   now.Add(-10 * time.Minute),
		EndTime:     now,
	}

	report, err := eng.Analyze(context.Background(), incident)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	found := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "waf:") && strings.Contains(w, "waf upstream down") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a waf warning, got warnings=%v", report.Warnings)
	}
	if report.Signals != nil && report.Signals.WAFFacts != nil {
		t.Fatalf("expected nil WAF facts on failure, got %+v", report.Signals.WAFFacts)
	}
}
