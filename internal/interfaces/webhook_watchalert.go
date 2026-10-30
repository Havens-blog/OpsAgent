// WatchAlert webhook integration — the platform's alert module (watch-alert)
// pushes native AlertCurEvent JSON to this endpoint via its WebHook / SREFlow
// notice medium, and OpsAgent turns each alert into an incident and runs the
// SAME deterministic RCA engine used by POST /api/v1/rca/analyze.
//
// Contract (watch-alert alert/consumer/handle.go generateAlertContent):
//
//	POST {hook}  Content-Type: application/json
//	{
//	  "alarm":     { ...AlertCurEvent... },   // tenantId,eventId,rule_name,
//	  "dutyUsers": [ {email,mobile,userId,username} ]
//	}
//
// Recovery notifications (is_recovered=true / status=recovered) are
// acknowledged but NOT analyzed — they are not new incidents.
package interfaces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// watchAlertPayload is the top-level body watch-alert posts for a WebHook /
// SREFlow notice. DutyUsers is intentionally not modelled field-by-field: the
// RCA engine keys off the alarm, not the on-call roster.
type watchAlertPayload struct {
	Alarm     *watchAlertEvent  `json:"alarm"`
	DutyUsers []json.RawMessage `json:"dutyUsers"`
}

// watchAlertEvent is the subset of watch-alert's models.AlertCurEvent that
// OpsAgent needs. Fields are deliberately snake_case to match watch-alert's
// json tags exactly.
type watchAlertEvent struct {
	TenantId         string                 `json:"tenantId"`
	EventId          string                 `json:"eventId"`
	RuleName         string                 `json:"rule_name"`
	DatasourceType   string                 `json:"datasource_type"`
	DatasourceId     string                 `json:"datasource_id"`
	Fingerprint      string                 `json:"fingerprint"`
	Severity         string                 `json:"severity"`
	Labels           map[string]interface{} `json:"labels"`
	SearchQL         string                 `json:"searchQL"`
	Annotations      string                 `json:"annotations"`
	IsRecovered      bool                   `json:"is_recovered"`
	FirstTriggerTime int64                  `json:"first_trigger_time"`
	RecoverTime      int64                  `json:"recover_time"`
	DutyUser         string                 `json:"duty_user"`
	FaultCenterId    string                 `json:"faultCenterId"`
	Status           string                 `json:"status"`
}

// watchAlertWebhookHandler receives a native watch-alert event, maps it to an
// OpsAgent incident, and kicks off RCA in the background (mirroring
// rcaAnalyzeHandler). Machine-to-machine: not bearer-protected; the platform
// (nginx) and an optional shared-secret header can gate it in production.
func (s *APIServer) watchAlertWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed", "")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to read body", err.Error())
		return
	}

	var payload watchAlertPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode watch-alert payload", err.Error())
		return
	}
	if payload.Alarm == nil {
		// watch-alert's "测试" button posts {"text": ...} (no alarm) to verify
		// connectivity, and its WebHookSender treats any non-200 as failure.
		// Acknowledge it with 200 so the test succeeds; nothing to analyze.
		s.logger.Warn("WatchAlert webhook without alarm event (treated as connectivity test)")
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"status":  "acknowledged",
			"skipped": "no_alarm",
		})
		return
	}
	event := payload.Alarm

	// Recovery is not a new incident — acknowledge it and close the incident
	// lifecycle by flipping the stored report's status to "resolved" (matched
	// by eventId; the same alert carries the same eventId across firing and
	// recovery). A no-op when the incident was never stored under this tenant.
	if event.IsRecovered || event.Status == "recovered" {
		if event.EventId != "" {
			tid := s.watchAlertTenant(event.TenantId)
			ctx := tenant.WithID(context.Background(), tid)
			if err := s.rcaStore.UpdateStatus(ctx, event.EventId, "resolved"); err != nil {
				s.logger.Warn("Recovery for unknown incident",
					observability.String("incident_id", event.EventId),
					observability.Err(err),
				)
			}
		}
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"status":  "acknowledged",
			"skipped": "recovered",
		})
		return
	}

	incident, err := event.toIncident()
	if err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_incident", "Invalid watch-alert event", err.Error())
		return
	}

	s.logger.Info("WatchAlert webhook received",
		observability.String("incident_id", incident.ID),
		observability.String("rule_name", event.RuleName),
		observability.String("severity", incident.Severity),
		observability.String("source", "watch-alert"),
	)

	// Carry the alert's tenant into the background analysis: the RCA store
	// scopes reports per tenant (tenant.IDFrom(ctx)), and the webhook has no
	// EIAM session. watch-alert's tenantId shares the platform tenant namespace
	// (e.g. "3" = 嘉立创 = EIAM tenant 3), so we store under it directly.
	// Non-numeric/empty tenantIds fall back to the configured default.
	tid := s.watchAlertTenant(event.TenantId)

	// Run RCA in the background so the webhook returns immediately (watch-alert
	// treats a non-200 as a delivery failure and would retry).
	go func() {
		ctx := tenant.WithID(context.Background(), tid)

		report, err := s.rcaEngine.Analyze(ctx, incident)
		if err != nil {
			s.logger.Error("WatchAlert-triggered RCA failed",
				observability.String("incident_id", incident.ID),
				observability.Err(err),
			)
			_ = s.rcaStore.UpdateStatus(ctx, incident.ID, "failed")
			return
		}

		if err := s.rcaStore.Store(ctx, report); err != nil {
			s.logger.Error("Failed to store RCA report",
				observability.String("incident_id", incident.ID),
				observability.Err(err),
			)
			return
		}
		_ = s.rcaStore.UpdateStatus(ctx, incident.ID, "completed")

		s.logger.Info("WatchAlert-triggered RCA completed",
			observability.String("incident_id", incident.ID),
			observability.Float64("confidence", report.Confidence),
			observability.Int("candidates", len(report.Candidates)),
		)

		// P0.2: close the loop — push the completed analysis to IM.
		facts := wafNoticeExtra(report)
		if facts != "" {
			s.logger.Info("WAF facts attached to RCA notification",
				observability.String("incident_id", incident.ID),
				observability.String("facts", facts),
			)
		}
		s.notifyRCACompleted(report, facts)
	}()

	// Return 200 (not 202) — watch-alert's WebHookSender treats any non-200 as
	// a delivery failure, so 202 would make it report "发送失败" on success.
	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"incident_id": incident.ID,
		"status":      "accepted",
		"message":     "RCA analysis started",
	})
}

// toIncident maps a watch-alert event onto the OpsAgent incident model and
// validates it. It normalizes severity to P0-P4 (Incident.Validate rejects
// anything else) and derives a non-zero [start,end] window.
func (e *watchAlertEvent) toIncident() (*models.Incident, error) {
	start := time.Now()
	if e.FirstTriggerTime > 0 {
		start = time.Unix(e.FirstTriggerTime, 0)
	}
	end := time.Now()
	if e.IsRecovered && e.RecoverTime > 0 {
		end = time.Unix(e.RecoverTime, 0)
	}
	if !end.After(start) {
		end = start.Add(time.Minute)
	}

	id := e.EventId
	if id == "" {
		id = e.Fingerprint
	}
	if id == "" {
		id = fmt.Sprintf("w8t-%d", time.Now().UnixNano())
	}

	incident := &models.Incident{
		ID:               id,
		DetectedAt:       time.Now(),
		StartTime:        start,
		EndTime:          end,
		Severity:         normalizeSeverity(e.Severity),
		AffectedServices: extractServices(e.Labels, e.RuleName, e.DatasourceType),
		Description:      e.description(),
		Source:           "watch-alert",
		Labels:           e.labelMap(),
	}
	return incident, incident.Validate()
}

// description joins the alert's annotations, rule name, and search query into
// the incident's initial description (RCA's "initial description or message").
func (e *watchAlertEvent) description() string {
	parts := make([]string, 0, 3)
	if e.Annotations != "" {
		parts = append(parts, e.Annotations)
	}
	if e.RuleName != "" {
		parts = append(parts, "规则: "+e.RuleName)
	}
	if e.SearchQL != "" {
		parts = append(parts, "查询: "+e.SearchQL)
	}
	if len(parts) == 0 {
		return "watch-alert 告警"
	}
	return joinNonEmpty(parts, "；")
}

// labelMap carries watch-alert provenance into the incident's labels so the
// RCA report and console retain the source trace.
func (e *watchAlertEvent) labelMap() map[string]string {
	m := map[string]string{
		"status":           "running",
		"source":           "watch-alert",
		"rule_name":        e.RuleName,
		"tenant_id":        e.TenantId,
		"fault_center_id":  e.FaultCenterId,
		"datasource_type":  e.DatasourceType,
		"fingerprint":      e.Fingerprint,
	}
	if e.IsRecovered {
		m["status"] = "resolved"
	}
	return m
}

// watchAlertTenant returns the OpsAgent tenant bucket that a watch-alert
// incident is stored under. watch-alert's tenantId shares the platform tenant
// namespace with EIAM (e.g. "3" = 嘉立创 = EIAM tenant 3), so we prefer the
// alert's own tenantId. When it is absent / non-numeric, we fall back to the
// configured watchalert.tenant_id (default 1). The alert's tenantId is also
// preserved as a label via labelMap for traceability.
func (s *APIServer) watchAlertTenant(raw string) int64 {
	if raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v != 0 {
			return v
		}
	}
	if s.settings != nil && s.settings.WatchAlert.TenantID != 0 {
		return s.settings.WatchAlert.TenantID
	}
	return 1
}

// normalizeSeverity maps watch-alert's severity vocabulary onto OpsAgent's
// P0-P4 (the only values Incident.Validate accepts). watch-alert's runtime
// uses P0/P1/P2; the "critical"/"warning"/"info" forms appear in its legacy
// data and are folded in defensively.
func normalizeSeverity(s string) string {
	switch s {
	case "P0", "P1", "P2", "P3", "P4":
		return s
	case "critical", "Critical", "严重":
		return "P1"
	case "warning", "Warning", "警告":
		return "P3"
	case "info", "Info", "提示":
		return "P4"
	default:
		return "P3"
	}
}

// extractServices pulls affected-service names from alert labels using the
// conventional keys, dedupes, and falls back to the rule name / datasource
// type when nothing is found (so the RCA engine always has at least one
// service to anchor topology scoring on).
func extractServices(labels map[string]interface{}, ruleName, datasourceType string) []string {
	keys := []string{"service", "app", "application", "job", "instance", "pod", "hostname", "namespace", "server"}
	seen := map[string]bool{}
	var out []string

	add := func(v string) {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}

	for _, k := range keys {
		if v, ok := labels[k]; ok {
			if s, ok := v.(string); ok {
				add(s)
			} else if s, ok := v.(fmt.Stringer); ok {
				add(s.String())
			}
		}
	}

	if len(out) == 0 && ruleName != "" {
		add(ruleName)
	}
	if len(out) == 0 && datasourceType != "" {
		add(datasourceType)
	}
	if len(out) == 0 {
		add("unknown")
	}
	return out
}

// notifyRCACompleted sends a best-effort IM summary after RCA finishes, using
// the stored notification config. Empty webhook/platform silently no-ops so an
// unconfigured notification channel never breaks the analysis path.
func (s *APIServer) notifyRCACompleted(report *models.RCAReport, extra string) {
	cfg := s.settings.ToNotificationConfig()
	if cfg.WebhookURL == "" {
		return
	}
	platform := cfg.Platform
	if platform == "" {
		platform = "dingtalk"
	}

	incidentID := ""
	severity := ""
	if report.Incident != nil {
		incidentID = report.Incident.ID
		severity = report.Incident.Severity
	}
	// WAF attack facts first: they directly answer the alarm's ask (attack
	// types + sources), while the engine's cross-dimension hypotheses are
	// supporting signals (the engine itself has no WAF dimension).
	text := fmt.Sprintf("[%s] 告警 #%s RCA 完成", severity, incidentID)
	if extra != "" {
		text += "\n\n" + extra
	}
	text += "\n\n" + report.GetSummary()

	s.logger.Info("RCA completion notification",
		observability.String("incident_id", incidentID),
		observability.String("text", text),
	)

	_, _, err := deliverIMMessage(platform, cfg.WebhookURL, cfg.Secret, text)
	if err != nil {
		s.logger.Error("Failed to notify RCA completion",
			observability.String("incident_id", incidentID),
			observability.Err(err),
		)
	}
}

// joinNonEmpty joins non-empty strings with sep.
func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i > 0 && out != "" {
			out += sep
		}
		out += p
	}
	return out
}

// wafNoticeExtra surfaces the engine's WAF facts in the IM notice only when
// the summary would not already carry them. WAF is a scored RCA dimension now,
// so the engine fetches these facts itself — re-querying here would duplicate
// both the call and the text on-call has to read.
func wafNoticeExtra(report *models.RCAReport) string {
	if report == nil || report.Signals == nil || report.Signals.WAFFacts == nil {
		return ""
	}
	if report.TopCandidate != nil &&
		report.TopCandidate.Hypothesis.Type == models.HypothesisTypeWAFAttack {
		return "" // the top candidate's evidence already lists these facts
	}
	// Includes the honest "window held no attack entries" case, which the
	// root-cause section cannot express (no hypothesis is generated for it).
	return report.Signals.WAFFacts.Text()
}
