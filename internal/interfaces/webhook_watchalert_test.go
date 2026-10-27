package interfaces

import (
	"testing"
	"time"
)

func TestNormalizeSeverity(t *testing.T) {
	cases := []struct{ in, want string }{
		{"P0", "P0"}, {"P1", "P1"}, {"P2", "P2"}, {"P3", "P3"}, {"P4", "P4"},
		{"critical", "P1"}, {"Critical", "P1"}, {"严重", "P1"},
		{"warning", "P3"}, {"Warning", "P3"}, {"警告", "P3"},
		{"info", "P4"}, {"Info", "P4"}, {"提示", "P4"},
		{"", "P3"}, {"weird", "P3"},
	}
	for _, c := range cases {
		if got := normalizeSeverity(c.in); got != c.want {
			t.Errorf("normalizeSeverity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractServices(t *testing.T) {
	t.Run("label keys", func(t *testing.T) {
		labels := map[string]interface{}{
			"service":  "order-service",
			"instance": "order-service-0",
			"pod":      "order-service-0",
		}
		got := extractServices(labels, "", "")
		if len(got) != 2 || got[0] != "order-service" || got[1] != "order-service-0" {
			t.Errorf("extractServices = %v, want [order-service order-service-0]", got)
		}
	})

	t.Run("fallback rule name", func(t *testing.T) {
		got := extractServices(map[string]interface{}{}, "cpu-high-rule", "")
		if len(got) != 1 || got[0] != "cpu-high-rule" {
			t.Errorf("extractServices = %v, want [cpu-high-rule]", got)
		}
	})

	t.Run("fallback datasource", func(t *testing.T) {
		got := extractServices(map[string]interface{}{}, "", "prometheus")
		if len(got) != 1 || got[0] != "prometheus" {
			t.Errorf("extractServices = %v, want [prometheus]", got)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		got := extractServices(map[string]interface{}{}, "", "")
		if len(got) != 1 || got[0] != "unknown" {
			t.Errorf("extractServices = %v, want [unknown]", got)
		}
	})
}

func TestToIncident(t *testing.T) {
	now := time.Now()
	start := now.Add(-10 * time.Minute).Unix()

	e := &watchAlertEvent{
		EventId:          "evt-123",
		RuleName:         "HighErrorRate",
		DatasourceType:   "metrics",
		Fingerprint:      "fp-abc",
		Severity:         "P1",
		Labels:           map[string]interface{}{"service": "order-service"},
		Annotations:      "错误率超过阈值",
		SearchQL:         "rate(errors[5m])",
		IsRecovered:      false,
		FirstTriggerTime: start,
		FaultCenterId:    "fc-1",
		Status:           "alerting",
	}

	inc, err := e.toIncident()
	if err != nil {
		t.Fatalf("toIncident error: %v", err)
	}

	if inc.ID != "evt-123" {
		t.Errorf("ID = %q, want evt-123", inc.ID)
	}
	if inc.Severity != "P1" {
		t.Errorf("Severity = %q, want P1", inc.Severity)
	}
	if inc.Source != "watch-alert" {
		t.Errorf("Source = %q, want watch-alert", inc.Source)
	}
	if len(inc.AffectedServices) != 1 || inc.AffectedServices[0] != "order-service" {
		t.Errorf("AffectedServices = %v, want [order-service]", inc.AffectedServices)
	}
	if !inc.StartTime.Equal(time.Unix(start, 0)) {
		t.Errorf("StartTime = %v, want %v", inc.StartTime, time.Unix(start, 0))
	}
	if inc.EndTime.Before(inc.StartTime) {
		t.Errorf("EndTime %v before StartTime %v", inc.EndTime, inc.StartTime)
	}
	if inc.Labels["rule_name"] != "HighErrorRate" {
		t.Errorf("Labels[rule_name] = %q", inc.Labels["rule_name"])
	}
	if inc.Labels["fault_center_id"] != "fc-1" {
		t.Errorf("Labels[fault_center_id] = %q", inc.Labels["fault_center_id"])
	}
}

func TestToIncidentRecoveredWindow(t *testing.T) {
	start := time.Now().Add(-30 * time.Minute).Unix()
	recover := time.Now().Add(-5 * time.Minute).Unix()

	e := &watchAlertEvent{
		EventId:          "evt-rec",
		Severity:         "P2",
		IsRecovered:      true,
		FirstTriggerTime: start,
		RecoverTime:      recover,
	}

	inc, err := e.toIncident()
	if err != nil {
		t.Fatalf("toIncident error: %v", err)
	}
	if !inc.EndTime.Equal(time.Unix(recover, 0)) {
		t.Errorf("EndTime = %v, want %v (recover time)", inc.EndTime, time.Unix(recover, 0))
	}
}

func TestToIncidentEmptyID(t *testing.T) {
	e := &watchAlertEvent{
		Severity:    "P3",
		Labels:      map[string]interface{}{},
		Annotations: "x",
	}
	inc, err := e.toIncident()
	if err != nil {
		t.Fatalf("toIncident error: %v", err)
	}
	if inc.ID == "" {
		t.Errorf("expected generated non-empty ID")
	}
	if inc.Severity != "P3" {
		t.Errorf("Severity = %q, want P3", inc.Severity)
	}
	if len(inc.AffectedServices) != 1 || inc.AffectedServices[0] != "unknown" {
		t.Errorf("AffectedServices = %v, want [unknown]", inc.AffectedServices)
	}
}

func TestDescription(t *testing.T) {
	e := &watchAlertEvent{
		Annotations: "数据库连接池耗尽",
		RuleName:    "DBConnPool",
		SearchQL:    "rate(db_errors)",
	}
	d := e.description()
	if d == "" {
		t.Errorf("description is empty")
	}
	// Order: annotations; 规则; 查询
	if d[0:len("数据库连接池耗尽")] != "数据库连接池耗尽" {
		t.Errorf("description = %q, expected to start with annotations", d)
	}
}
