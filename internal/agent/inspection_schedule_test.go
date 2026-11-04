package agent

import (
	"errors"
	"testing"
	"time"
)

func TestCronMatches(t *testing.T) {
	cases := []struct {
		name string
		cron string
		t    time.Time
		want bool
	}{
		{"every 15 min hits", "*/15 * * * *", time.Date(2026, 1, 5, 9, 15, 0, 0, time.UTC), true},
		{"every 15 min miss", "*/15 * * * *", time.Date(2026, 1, 5, 9, 7, 0, 0, time.UTC), false},
		{"weekday 9am", "0 9 * * 1-5", time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), true},   // Monday
		{"weekend miss", "0 9 * * 1-5", time.Date(2026, 1, 4, 9, 0, 0, 0, time.UTC), false}, // Sunday
		{"midnight daily", "0 0 * * *", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), true},
	}
	for _, tc := range cases {
		sch := &InspectionSchedule{Cron: tc.cron}
		if got := sch.Matches(tc.t); got != tc.want {
			t.Errorf("%s: Matches(%v) = %v, want %v", tc.name, tc.t, got, tc.want)
		}
	}
}

func TestCronInvalid(t *testing.T) {
	sch := &InspectionSchedule{Cron: "not a cron"}
	if sch.Matches(time.Now()) {
		t.Error("invalid cron should never match")
	}
	if _, err := parseCron("0 9 * *"); err == nil {
		t.Error("expected error for 4-field cron")
	}
	if _, err := parseCron("* * * * 99"); err == nil {
		t.Error("expected error for out-of-range dow")
	}
}

func TestClassifyInspectionRun(t *testing.T) {
	if got := classifyInspectionRun(errors.New("boom"), false); got != InspectionRunFailed {
		t.Errorf("failed = %v", got)
	}
	if got := classifyInspectionRun(nil, false); got != InspectionRunUnmet {
		t.Errorf("unmet = %v", got)
	}
	if got := classifyInspectionRun(nil, true); got != InspectionRunSuccess {
		t.Errorf("success = %v", got)
	}
}

func TestInspectionScheduleShouldContinue(t *testing.T) {
	sch := &InspectionSchedule{MaxRuns: 2}
	if sch.recordRun(nil, false) != InspectionRunUnmet {
		t.Fatal("expected unmet")
	}
	if !sch.shouldContinue() {
		t.Error("should continue after unmet within max runs")
	}
	if sch.recordRun(nil, false) != InspectionRunUnmet {
		t.Fatal("expected unmet")
	}
	if sch.shouldContinue() {
		t.Error("should stop when max runs reached")
	}

	sch2 := &InspectionSchedule{MaxRuns: 5}
	sch2.recordRun(nil, true)
	if sch2.shouldContinue() {
		t.Error("should stop after success (stop condition met)")
	}
}

func TestStopConditionMet(t *testing.T) {
	summary := InspectionSummary{Score: 95, CriticalCount: 0}
	if !StopConditionMet("", summary) {
		t.Error("empty condition should be met")
	}
	if !StopConditionMet("no_critical", summary) {
		t.Error("no_critical should be met with 0 critical")
	}
	if !StopConditionMet("score>=90", summary) {
		t.Error("score>=90 should be met with score 95")
	}
	if StopConditionMet("no_critical", InspectionSummary{CriticalCount: 1}) {
		t.Error("no_critical should not be met with critical findings")
	}
	if StopConditionMet("score>=99", summary) {
		t.Error("score>=99 should not be met with score 95")
	}
	if StopConditionMet("unknown_condition", summary) {
		t.Error("unknown condition should not be met")
	}
}
