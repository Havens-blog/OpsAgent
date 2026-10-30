package interfaces

import (
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

func TestWAFNoticeExtraMissingFactsForWAFAlarm(t *testing.T) {
	report := &models.RCAReport{
		Incident: &models.Incident{
			ID:          "inc-1",
			Severity:    "P2",
			Description: "阿里云WAF：域名 www.jlc-smt.com-waf 最近30分钟CC攻击拦截量峰值超过1000次",
			StartTime:   time.Now().Add(-10 * time.Minute),
			EndTime:     time.Now(),
		},
		Signals:      &models.Signals{}, // WAFFacts nil
		TopCandidate: &models.ScoredHypothesis{Hypothesis: models.Hypothesis{Type: models.HypothesisTypeMetricAnomaly}},
	}

	extra := wafNoticeExtra(report)
	if !strings.Contains(extra, "WAF 事件") || !strings.Contains(extra, "不可信") {
		t.Fatalf("expected WAF-missing caveat, got %q", extra)
	}
}

func TestWAFNoticeExtraHidesCaveatWhenNotWAFAlarm(t *testing.T) {
	report := &models.RCAReport{
		Incident: &models.Incident{ID: "inc-2", Description: "某服务内存使用率异常"},
		Signals:  &models.Signals{}, // WAFFacts nil, but not a WAF alarm
	}
	if extra := wafNoticeExtra(report); extra != "" {
		t.Fatalf("expected empty extra for non-WAF alarm, got %q", extra)
	}
}
