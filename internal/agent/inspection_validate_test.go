package agent

import "testing"

func TestValidateStopCondition(t *testing.T) {
	for _, ok := range []string{"", "no_critical", "score>=90", " score>= 80.5 "} {
		if err := ValidateStopCondition(ok); err != nil {
			t.Errorf("ValidateStopCondition(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"score>=abc", "unknown", "critical<1"} {
		if err := ValidateStopCondition(bad); err == nil {
			t.Errorf("ValidateStopCondition(%q) = nil, want error", bad)
		}
	}
}

func TestValidateCron(t *testing.T) {
	if err := ValidateCron("0 9 * * 1-5"); err != nil {
		t.Errorf("valid cron rejected: %v", err)
	}
	if err := ValidateCron("0 9 * *"); err == nil {
		t.Error("4-field cron accepted")
	}
}
