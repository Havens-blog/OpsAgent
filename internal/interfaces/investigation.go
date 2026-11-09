package interfaces

import (
	"context"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// alertInvestigator 执行调用方规划好的多专家只读取证
type alertInvestigator interface {
	Investigate(ctx context.Context, task *agent.Task, agents []string) *agent.ExecutionResult
}

// investigationTimeout 单次告警调查的总时限
const investigationTimeout = 5 * time.Minute

// investigateIncident 是告警与手工 RCA 的统一调查入口：有两个以上专家时委派
// Coordinator 并行取证，复用 monitor 的确定性报告；否则回退确定性引擎。
// 部分失败的覆盖缺口由程序写入 Investigation，存储与通知读取同一份结果。
func (s *APIServer) investigateIncident(ctx context.Context, incident *models.Incident) (*models.RCAReport, error) {
	if s.investigator == nil || len(s.investigationAgents) < 2 {
		return s.rcaEngine.Analyze(ctx, incident)
	}

	ictx, cancel := context.WithTimeout(ctx, investigationTimeout)
	res := s.investigator.Investigate(ictx, incidentTask(incident), s.investigationAgents)
	cancel()
	outcome := investigationOutcome(res)

	if rep := monitorReport(res); rep != nil {
		out := *rep
		// 保留 monitor 引擎解析后的 incident（含域名解析出的后端服务），
		// 不覆盖为原始 incident，否则丢失实体解析结果与映射来源。
		if rep.Incident != nil {
			out.Incident = rep.Incident
		} else {
			out.Incident = incident
		}
		out.Investigation = outcome
		return &out, nil
	}

	// RCA 专家未产出报告时回退确定性引擎，但保留其他专家的调查结论
	report, err := s.rcaEngine.Analyze(ctx, incident)
	if err != nil {
		return nil, err
	}
	report.Investigation = outcome
	return report, nil
}

// incidentTask 把事件正规化为专家可直接消费的任务参数
func incidentTask(inc *models.Incident) *agent.Task {
	services := make([]interface{}, 0, len(inc.AffectedServices))
	for _, svc := range inc.AffectedServices {
		services = append(services, svc)
	}
	start := inc.StartTime.Format(time.RFC3339)
	end := inc.EndTime.Format(time.RFC3339)

	params := map[string]interface{}{
		"incident": map[string]interface{}{
			"id":                inc.ID,
			"start_time":        start,
			"end_time":          end,
			"severity":          inc.Severity,
			"description":       inc.Description,
			"affected_services": services,
		},
		"start_time": start,
		"end_time":   end,
		// 完整受影响服务列表：专家据此明确调查范围，避免只取第一个服务
		// 而遗漏解析出的后端或其它服务。
		"services": services,
	}
	if len(inc.AffectedServices) > 0 {
		params["service"] = inc.AffectedServices[0]
	}

	return &agent.Task{
		ID:          inc.ID,
		Description: inc.Description,
		Params:      params,
		CreatedAt:   time.Now(),
		Status:      "pending",
	}
}

// monitorReport 取出成功的 RCA 专家报告，避免确定性引擎重复分析
func monitorReport(res *agent.ExecutionResult) *models.RCAReport {
	if res == nil {
		return nil
	}
	subs, _ := res.Data["submissions"].([]*agent.TaskSubmission)
	for _, sub := range subs {
		if sub == nil || sub.AgentName != "monitor" || sub.Error != "" || sub.Result == nil || !sub.Result.Success {
			continue
		}
		if rep, ok := sub.Result.Data["report"].(*models.RCAReport); ok && rep != nil {
			return rep
		}
	}
	return nil
}

// investigationSummary 渲染调查结论文本，失败专家由程序强制披露，不依赖综合文本
func investigationSummary(res *agent.ExecutionResult) string {
	if res == nil {
		return ""
	}
	text := strings.TrimSpace(res.Response)
	failed, _ := res.Data["failed_agents"].([]string)
	// Coordinator 已附加缺口说明时不重复追加
	if notice := agent.CoverageGapNotice(failed); notice != "" && !strings.Contains(text, notice) {
		text += "\n\n" + notice
	}
	return strings.TrimSpace(text)
}

// investigationOutcome 从调查执行结果构造结构化裁决，综合 verdict 成为最终结论来源。
func investigationOutcome(res *agent.ExecutionResult) *models.InvestigationOutcome {
	if res == nil {
		return nil
	}
	outcome := &models.InvestigationOutcome{
		Status:  "completed",
		Verdict: "undetermined",
		Summary: investigationSummary(res),
	}
	if failed, ok := res.Data["failed_agents"].([]string); ok && len(failed) > 0 {
		outcome.Status = "partial"
		outcome.MissingSources = append([]string(nil), failed...)
	}
	if partial, ok := res.Data["partial"].(bool); ok && partial {
		outcome.Status = "partial"
	}
	if v, ok := res.Data["verdict"].(string); ok && v == "determined" {
		outcome.Verdict = "determined"
		if rc, ok := res.Data["root_cause"].(string); ok {
			outcome.RootCause = rc
		}
	}
	return outcome
}
