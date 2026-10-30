package interfaces

import (
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

func TestReportToIncidentDTOIncludesWarnings(t *testing.T) {
	inc := &models.Incident{
		ID:          "inc-1",
		Severity:    "P1",
		Description: "connection pool exhausted",
		StartTime:   time.Now(),
		EndTime:     time.Now(),
	}

	withWarn := &models.RCAReport{Incident: inc, Warnings: []string{"logs: connection refused"}}
	dto := reportToIncidentDTO(withWarn)
	if len(dto.Warnings) != 1 || dto.Warnings[0] != "logs: connection refused" {
		t.Fatalf("warnings = %v, want [logs: connection refused]", dto.Warnings)
	}

	noWarn := &models.RCAReport{Incident: inc}
	dto2 := reportToIncidentDTO(noWarn)
	if dto2.Warnings == nil {
		t.Fatalf("expected non-nil empty warnings slice, got nil")
	}
	if len(dto2.Warnings) != 0 {
		t.Fatalf("warnings = %v, want empty", dto2.Warnings)
	}
}
