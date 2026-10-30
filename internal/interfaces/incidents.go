// Incident list/detail endpoints — expose the incidents embedded in stored RCA
// reports so the console's incident management pages reflect real analyses
// (manual /rca/analyze AND watch-alert-triggered) instead of demo data.
package interfaces

import (
	"net/http"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// incidentDTO matches the frontend `Incident` type (snake_case).
type incidentDTO struct {
	ID               string   `json:"id"`
	Severity         string   `json:"severity"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	StartTime        string   `json:"start_time"`
	EndTime          string   `json:"end_time,omitempty"`
	Status           string   `json:"status"`
	AffectedServices []string `json:"affected_services,omitempty"`
	RootCause        string   `json:"root_cause,omitempty"`
	Confidence       float64  `json:"confidence,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
}

// incidentDetailDTO adds evidence + timeline for the detail page.
type incidentDetailDTO struct {
	incidentDTO
	Evidence []incidentEvidenceDTO `json:"evidence,omitempty"`
	Timeline []incidentTimelineDTO `json:"timeline,omitempty"`
}

// incidentEvidenceDTO matches the frontend `Evidence` type.
type incidentEvidenceDTO struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	Description string `json:"description"`
}

// incidentTimelineDTO is a single key event on the incident timeline.
type incidentTimelineDTO struct {
	Time  string `json:"time"`
	Event string `json:"event"`
}

// incidentsListHandler returns all incidents (from stored RCA reports) for the
// current tenant.
func (s *APIServer) incidentsListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
		return
	}

	reports, err := s.rcaStore.List(r.Context())
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to list incidents", err.Error())
		return
	}

	result := make([]incidentDTO, 0, len(reports))
	for _, report := range reports {
		if report.Incident == nil {
			continue
		}
		result = append(result, reportToIncidentDTO(report))
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"incidents": result,
		"count":     len(result),
	})
}

// incidentDetailHandler returns one incident with evidence and timeline.
func (s *APIServer) incidentDetailHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
		return
	}

	incidentID := strings.TrimPrefix(r.URL.Path, "/api/v1/incidents/")
	if incidentID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "missing_id", "Incident ID is required", "")
		return
	}

	report, err := s.rcaStore.Get(r.Context(), incidentID)
	if err != nil {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Incident not found", err.Error())
		return
	}
	if report.Incident == nil {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Incident not found", "")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, reportToIncidentDetailDTO(report))
}

// reportToIncidentDTO maps an RCA report onto the list `Incident` shape.
func reportToIncidentDTO(report *models.RCAReport) incidentDTO {
	inc := report.Incident
	dto := incidentDTO{
		ID:               inc.ID,
		Severity:         inc.Severity,
		Title:            incidentTitle(inc),
		Description:      inc.Description,
		StartTime:        inc.StartTime.Format(time.RFC3339),
		EndTime:          formatTimePtr(inc.EndTime),
		Status:           incidentStatus(inc),
		AffectedServices: inc.AffectedServices,
		Warnings:         normalizeWarnings(report.Warnings),
	}
	if report.TopCandidate != nil {
		dto.RootCause = report.TopCandidate.Hypothesis.Description
		dto.Confidence = report.TopCandidate.Confidence
	} else {
		dto.Confidence = report.Confidence
	}
	return dto
}

// reportToIncidentDetailDTO maps a report onto the detail-page shape, deriving
// evidence from the scored candidates and a minimal timeline from timestamps.
func reportToIncidentDetailDTO(report *models.RCAReport) incidentDetailDTO {
	dto := incidentDetailDTO{
		incidentDTO: reportToIncidentDTO(report),
		Evidence:    evidenceFromReport(report),
		Timeline:    timelineFromReport(report),
	}
	return dto
}

// incidentTitle derives a short human title: watch-alert rule name when
// present, else the first line of the description, else a generic label.
func incidentTitle(inc *models.Incident) string {
	if inc.Labels != nil {
		if rule := inc.Labels["rule_name"]; rule != "" {
			return rule
		}
	}
	if inc.Description != "" {
		// First line (up to first newline), bounded to a readable length.
		line := inc.Description
		if i := strings.IndexAny(line, "\n"); i >= 0 {
			line = line[:i]
		}
		if len(line) > 60 {
			line = line[:60] + "…"
		}
		return line
	}
	return "事件 " + inc.ID
}

// incidentStatus maps the stored RCA status label onto the incident lifecycle
// vocabulary the console renders (active vs resolved). Analysis-progress states
// (running/completed/failed) all mean the incident is still open.
func incidentStatus(inc *models.Incident) string {
	if inc.Labels != nil {
		switch inc.Labels["status"] {
		case "resolved":
			return "resolved"
		case "failed", "completed", "running":
			return "active"
		}
	}
	return "active"
}

// evidenceFromReport flattens the scored candidates into console Evidence
// entries. Each candidate's hypothesis carries a service, a coarse type, and
// evidence strings; each string becomes one evidence row.
func evidenceFromReport(report *models.RCAReport) []incidentEvidenceDTO {
	start := report.Incident.StartTime.Format(time.RFC3339)
	end := formatTimePtr(report.Incident.EndTime)

	var out []incidentEvidenceDTO
	for _, c := range report.Candidates {
		etype := hypothesisTypeToEvidenceType(c.Hypothesis.Type)
		source := c.Hypothesis.Service
		if source == "" {
			source = "unknown"
		}
		evidence := c.Hypothesis.Evidence
		if len(evidence) == 0 {
			evidence = []string{c.Hypothesis.Description}
		}
		for _, e := range evidence {
			out = append(out, incidentEvidenceDTO{
				Type:        etype,
				Source:      source,
				StartTime:   start,
				EndTime:     end,
				Description: e,
			})
		}
	}
	return out
}

// timelineFromReport builds a minimal, honest timeline from the report's
// timestamps (alert start, detection, and analysis completion).
func timelineFromReport(report *models.RCAReport) []incidentTimelineDTO {
	inc := report.Incident
	timeline := []incidentTimelineDTO{
		{Time: inc.StartTime.Format(time.RFC3339), Event: "事件开始"},
	}
	if !inc.DetectedAt.IsZero() {
		timeline = append(timeline, incidentTimelineDTO{
			Time: inc.DetectedAt.Format(time.RFC3339), Event: "事件入库",
		})
	}
	if !report.GeneratedAt.IsZero() {
		timeline = append(timeline, incidentTimelineDTO{
			Time: report.GeneratedAt.Format(time.RFC3339), Event: "RCA 分析完成",
		})
	}
	return timeline
}

// hypothesisTypeToEvidenceType maps the RCA hypothesis type onto the console's
// evidence badge vocabulary (metric/log/trace/change/topology/resource).
func hypothesisTypeToEvidenceType(t models.HypothesisType) string {
	switch t {
	case models.HypothesisTypeMetricAnomaly:
		return "metric"
	case models.HypothesisTypeResource:
		return "resource"
	case models.HypothesisTypeErrorSpike, models.HypothesisTypeLatency:
		return "log"
	case models.HypothesisTypeChange:
		return "change"
	case models.HypothesisTypeDependency:
		return "topology"
	case models.HypothesisTypeWAFAttack:
		return "waf"
	default:
		return "log"
	}
}
