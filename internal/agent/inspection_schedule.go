package agent

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// InspectionRunStatus 区分巡检运行结果：执行失败(failed)与目标未达成(unmet)
type InspectionRunStatus string

const (
	InspectionRunSuccess InspectionRunStatus = "success"
	InspectionRunFailed  InspectionRunStatus = "failed"
	InspectionRunUnmet   InspectionRunStatus = "unmet"
)

// InspectionSchedule 是一次可调度巡检的声明与运行状态。
type InspectionSchedule struct {
	ID            string
	Cron          string
	StopCondition string
	MaxRuns       int
	runs          int
	lastStatus    InspectionRunStatus
}

// Matches 判断 cron 是否在 t 触发。
func (s *InspectionSchedule) Matches(t time.Time) bool {
	sch, err := parseCron(s.Cron)
	if err != nil {
		return false
	}
	return sch.matches(t)
}

// recordRun 记录一次运行并返回分类后的状态。
func (s *InspectionSchedule) recordRun(execErr error, stopMet bool) InspectionRunStatus {
	s.runs++
	s.lastStatus = classifyInspectionRun(execErr, stopMet)
	return s.lastStatus
}

// shouldContinue 判断是否继续：未达最大次数且上次运行是 unmet
func (s *InspectionSchedule) shouldContinue() bool {
	if s.MaxRuns > 0 && s.runs >= s.MaxRuns {
		return false
	}
	return s.lastStatus == InspectionRunUnmet
}

func classifyInspectionRun(execErr error, stopMet bool) InspectionRunStatus {
	if execErr != nil {
		return InspectionRunFailed
	}
	if !stopMet {
		return InspectionRunUnmet
	}
	return InspectionRunSuccess
}

// StopConditionMet 根据巡检摘要评估停止条件。空条件视为已满足。
func StopConditionMet(cond string, s InspectionSummary) bool {
	cond = strings.TrimSpace(cond)
	switch {
	case cond == "":
		return true
	case cond == "no_critical":
		return s.CriticalCount == 0
	case strings.HasPrefix(cond, "score>="):
		threshold, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(cond, "score>=")), 64)
		return err == nil && s.Score >= threshold
	default:
		return false
	}
}

// cronSchedule 是五段 cron 表达式的解析结果
type cronSchedule struct {
	minute, hour, dom, month, dow *cronField
}

func parseCron(expr string) (*cronSchedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron needs 5 fields, got %d", len(parts))
	}
	minute, err := parseCronField(parts[0], 0, 59)
	if err != nil {
		return nil, err
	}
	hour, err := parseCronField(parts[1], 0, 23)
	if err != nil {
		return nil, err
	}
	dom, err := parseCronField(parts[2], 1, 31)
	if err != nil {
		return nil, err
	}
	month, err := parseCronField(parts[3], 1, 12)
	if err != nil {
		return nil, err
	}
	dow, err := parseCronField(parts[4], 0, 6)
	if err != nil {
		return nil, err
	}
	return &cronSchedule{minute: minute, hour: hour, dom: dom, month: month, dow: dow}, nil
}

func (c *cronSchedule) matches(t time.Time) bool {
	return c.minute.matches(t.Minute()) &&
		c.hour.matches(t.Hour()) &&
		c.dom.matches(t.Day()) &&
		c.month.matches(int(t.Month())) &&
		c.dow.matches(int(t.Weekday()))
}

// cronField 是单个 cron 字段的匹配器，支持 *、数字、a-b、*/step 与逗号列表
type cronField struct {
	min, max int
	matchers []func(int) bool
}

func parseCronField(token string, min, max int) (*cronField, error) {
	f := &cronField{min: min, max: max}
	for _, part := range strings.Split(token, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty cron field part")
		}
		step := 0
		base := part
		if i := strings.Index(part, "/"); i >= 0 {
			base = part[:i]
			s, err := strconv.Atoi(part[i+1:])
			if err != nil || s <= 0 {
				return nil, fmt.Errorf("invalid cron step %q", part)
			}
			step = s
		}

		if base == "*" {
			if step > 0 {
				f.matchers = append(f.matchers, func(v int) bool { return (v-min)%step == 0 })
			} else {
				f.matchers = append(f.matchers, func(int) bool { return true })
			}
			continue
		}

		if i := strings.Index(base, "-"); i >= 0 {
			lo, err1 := strconv.Atoi(base[:i])
			hi, err2 := strconv.Atoi(base[i+1:])
			if err1 != nil || err2 != nil || lo < min || hi > max || lo > hi {
				return nil, fmt.Errorf("invalid cron range %q", part)
			}
			if step > 0 {
				f.matchers = append(f.matchers, func(v int) bool { return v >= lo && v <= hi && (v-lo)%step == 0 })
			} else {
				f.matchers = append(f.matchers, func(v int) bool { return v >= lo && v <= hi })
			}
			continue
		}

		n, err := strconv.Atoi(base)
		if err != nil || n < min || n > max {
			return nil, fmt.Errorf("invalid cron value %q", part)
		}
		f.matchers = append(f.matchers, func(v int) bool { return v == n })
	}
	return f, nil
}

func (f *cronField) matches(v int) bool {
	for _, m := range f.matchers {
		if m(v) {
			return true
		}
	}
	return false
}
