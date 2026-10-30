package models

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// FactCount is one aggregated WAF fact row (attack rule / source IP / URI / action).
type FactCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// WAFAttackFacts is the factual WAF attack summary for one domain+window,
// queried from the e-cam log federation. It is a first-class RCA signal
// (HypothesisTypeWAFAttack) and also renders into IM notifications.
type WAFAttackFacts struct {
	Domain string `json:"domain"`
	// Total is the number of entries actually sampled and aggregated below.
	Total int `json:"total"`
	// Matched is the federation-reported match count for the window, which can
	// far exceed the sample. Zero when the federation reports none.
	Matched int64 `json:"matched"`
	// SampleCapped marks that the sample hit the query cap (or the federation
	// truncated its fan-out), so the aggregates are a bounded sample — never
	// report them as the full picture.
	SampleCapped  bool        `json:"sample_capped"`
	WindowStart   time.Time   `json:"window_start"`
	WindowEnd     time.Time   `json:"window_end"`
	AttackRules   []FactCount `json:"attack_rules"`
	SourceIPs     []FactCount `json:"source_ips"`
	SourceIPCount int         `json:"source_ip_count"`
	TargetURIs    []FactCount `json:"target_uris"`
	Actions       []FactCount `json:"actions"`
}

// SummaryLine is the one-line description used as the hypothesis description.
func (f *WAFAttackFacts) SummaryLine() string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf("WAF 攻击进行中: %s %s;%s", f.Domain, f.CountPhrase(), f.Verdict())
}

// EvidenceLines renders the facts as RCA evidence strings.
func (f *WAFAttackFacts) EvidenceLines() []string {
	if f == nil || f.Total == 0 {
		return nil
	}
	return []string{
		fmt.Sprintf("WAF 攻击事实(%s %s~%s): %s",
			f.Domain, f.WindowStart.Format("01-02 15:04"), f.WindowEnd.Format("01-02 15:04"), f.CountPhrase()),
		"结论: " + f.Verdict(),
		"攻击类型: " + JoinFactCounts(f.AttackRules, 4),
		"攻击来源: " + f.SourceLine(),
		"攻击目标: " + joinURIs(f.TargetURIs, 4),
		"处置动作: " + JoinActionCounts(f.Actions, 4),
	}
}

// CountPhrase states the volume honestly: a capped sample must never be
// reported as the total (a 500-entry sample on a 500 cap means "at least").
func (f *WAFAttackFacts) CountPhrase() string {
	if f.Matched > int64(f.Total) {
		return fmt.Sprintf("窗口内匹配 %d 条攻击/处置日志(采样 %d 条做分布分析)", f.Matched, f.Total)
	}
	if f.SampleCapped {
		return fmt.Sprintf("命中 ≥%d 条攻击/处置日志(达采样上限,实际更多)", f.Total)
	}
	return fmt.Sprintf("命中 %d 条攻击/处置日志", f.Total)
}

// Verdict judges the disposition mix. "Blocked count" in an alarm title is not
// the same thing as blocking: observation-mode (alert/test) rules PASS the
// request through (they only log a potential attack), while block denies and
// js/captcha challenge it — a synthetic probe or bot cannot solve the
// challenge, so a challenge fails it just like a block.
func (f *WAFAttackFacts) Verdict() string {
	blocks, passes, alerts, challenges := f.actionSplit()
	switch {
	case passes > 0:
		return fmt.Sprintf("⚠ 其中 %d 条显式放行(pass),需人工复核", passes)
	case blocks == f.Total && blocks > 0:
		return fmt.Sprintf("已全部拦截(block),共 %d 条", blocks)
	case blocks > 0:
		line := fmt.Sprintf("拦截 %d · 挑战 %d", blocks, challenges)
		if alerts > 0 {
			line += fmt.Sprintf(" · 观察 %d(不阻断)", alerts)
		}
		if challenges > blocks {
			line += fmt.Sprintf(";以挑战/观察为主,实际拦截仅 %d 条", blocks)
		}
		return line
	case challenges > 0:
		return fmt.Sprintf("挑战处置 %d 条(须通过挑战才能访问,探针/爬虫会失败),无直接拦截", challenges)
	case alerts > 0:
		return fmt.Sprintf("观察模式 %d 条(仅记录不阻断),无实际拦截/挑战", alerts)
	default:
		return "未见明确处置动作"
	}
}

// actionSplit groups dispositions into block / pass / alert / challenge.
func (f *WAFAttackFacts) actionSplit() (blocks, passes, alerts, challenges int) {
	for _, a := range f.Actions {
		switch a.Key {
		case "block":
			blocks += a.Count
		case "pass":
			passes += a.Count
		case "alert":
			alerts += a.Count
		default:
			challenges += a.Count // js/captcha 等挑战类处置
		}
	}
	return
}

// Text renders the facts as an IM brief: verdict first, then one labeled line
// per key point. Raw count dumps without interpretation are unreadable on-call.
func (f *WAFAttackFacts) Text() string {
	if f == nil {
		return ""
	}
	win := f.WindowStart.Format("01-02 15:04") + "~" + f.WindowEnd.Format("01-02 15:04")
	if f.Total == 0 {
		return fmt.Sprintf("【WAF攻击事实】%s(%s)\n结论: 窗口内未查到攻击/处置记录(告警计数可能来自WAF指标侧,建议在WAF控制台复核)", f.Domain, win)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "【WAF攻击事实】%s(%s)\n", f.Domain, win)
	fmt.Fprintf(&b, "结论: %s,%s\n", f.CountPhrase(), f.Verdict())
	fmt.Fprintf(&b, "攻击类型: %s\n", JoinFactCounts(f.AttackRules, 4))
	fmt.Fprintf(&b, "攻击来源: %s\n", f.SourceLine())
	fmt.Fprintf(&b, "攻击目标: %s\n", joinURIs(f.TargetURIs, 4))
	fmt.Fprintf(&b, "处置动作: %s", JoinActionCounts(f.Actions, 4))
	return b.String()
}

// JoinActionCounts renders disposition counts with their operational meaning —
// "alert" reads as observation (request NOT blocked), which is the difference
// between "we are protected" and "we are only watching".
func JoinActionCounts(items []FactCount, n int) string {
	if len(items) == 0 {
		return "无记录"
	}
	if len(items) > n {
		items = items[:n]
	}
	labels := map[string]string{
		"block":   "拦截",
		"pass":    "放行",
		"alert":   "观察(未拦截)",
		"js":      "JS挑战",
		"captcha": "验证码挑战",
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		label := labels[it.Key]
		if label == "" {
			label = it.Key
		}
		parts = append(parts, fmt.Sprintf("%s %d", label, it.Count))
	}
	return strings.Join(parts, " · ")
}

// SourceLine characterizes attacker IPs: distinct count, top IP, and the
// dominant /24 segment when the top sources concentrate in one.
func (f *WAFAttackFacts) SourceLine() string {
	if len(f.SourceIPs) == 0 {
		return "无记录"
	}
	total := 0
	seg := map[string]int{}
	for _, ip := range f.SourceIPs {
		total += ip.Count
		if i := strings.LastIndex(ip.Key, "."); i > 0 {
			seg[ip.Key[:i]] += ip.Count
		}
	}
	line := fmt.Sprintf("Top1 %s(%d次)", f.SourceIPs[0].Key, f.SourceIPs[0].Count)
	if f.SourceIPCount > 0 {
		line += fmt.Sprintf(",样本内共 %d 个来源IP", f.SourceIPCount)
	}
	best, bestN := "", 0
	for s, n := range seg {
		if n > bestN {
			best, bestN = s, n
		}
	}
	if best != "" && total > 0 && bestN*100/total >= 60 {
		line += fmt.Sprintf(",集中于 %s.0/24 网段(Top10占%d%%)", best, bestN*100/total)
	}
	return line
}

// JoinFactCounts renders fact counts with on-call-readable rule labels.
func JoinFactCounts(items []FactCount, n int) string {
	if len(items) == 0 {
		return "无记录"
	}
	if len(items) > n {
		items = items[:n]
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s %d", HumanizeWAFRule(it.Key), it.Count))
	}
	return strings.Join(parts, " · ")
}

// HumanizeWAFRule translates WAF3 rule identifiers into readable attack types.
// Unknown values pass through verbatim — never invent meaning.
func HumanizeWAFRule(key string) string {
	if key == "" {
		return "未标注规则"
	}
	k := strings.TrimSuffix(key, "(test)")
	suffix := ""
	if k != key {
		suffix = "(观察模式)"
	}
	bare := map[string]string{
		"xss":              "XSS跨站脚本",
		"sqli":             "SQL注入",
		"lfilei":           "本地文件包含/穿越",
		"rfilei":           "远程文件包含",
		"scanner_behavior": "扫描器探测",
		"cmdi":             "命令注入",
		"codei":            "代码注入",
		"webshell":         "Webshell后门",
		"other":            "其他Web攻击",
	}
	switch {
	case strings.HasPrefix(k, "web_attack:"):
		if v, ok := bare[strings.TrimPrefix(k, "web_attack:")]; ok {
			return v + suffix
		}
	case strings.HasPrefix(k, "rule:"):
		if v, ok := bare[strings.TrimPrefix(k, "rule:")]; ok {
			return v + suffix
		}
	case strings.HasPrefix(k, "acl:"):
		return "自定义ACL规则" + suffix
	case strings.HasPrefix(k, "plugin:"):
		return "场景防护插件" + suffix
	case k == "cc":
		return "CC频率攻击" + suffix
	case k == "antiscan":
		return "扫描防护" + suffix
	}
	return k + suffix
}

// joinURIs lists top attacked paths readably: the empty-query placeholder is
// stripped and long URIs keep head+tail (attack payloads live at the tail).
func joinURIs(items []FactCount, n int) string {
	if len(items) == 0 {
		return "无记录"
	}
	out := make([]string, 0, n)
	for _, u := range items {
		if len(out) >= n {
			break
		}
		p := cleanURI(u.Key)
		if p == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%s(%d)", p, u.Count))
	}
	return strings.Join(out, " · ")
}

func cleanURI(u string) string {
	u = strings.TrimSuffix(strings.TrimSpace(u), "?-")
	u = strings.TrimSuffix(u, "?")
	const max = 64
	if len(u) <= max {
		return u
	}
	return u[:24] + "…" + u[len(u)-32:]
}

// wafDomainRe extracts the protected domain out of WAF alarm text
// ("域名 www.jlc-smt.com-waf ...").
var wafDomainRe = regexp.MustCompile(`域名[:：]?\s*([A-Za-z0-9._-]+)`)

// urlHostRe extracts the host out of a probe / dial-test target URL
// ("拨测任务：https://www.jlc-smt.com/144984416").
var urlHostRe = regexp.MustCompile(`https?://([A-Za-z0-9._-]+)`)

// ExtractWAFDomain returns the normalized host named in alarm or task text,
// or "" when none is present. It recognizes both WAF annotations ("域名
// xxx-waf") and probe/dial-test target URLs ("https://www.jlc-smt.com/...").
// Shared by the WatchAlert webhook and the RCA engine.
func ExtractWAFDomain(text string) string {
	if m := wafDomainRe.FindStringSubmatch(text); len(m) >= 2 {
		return NormalizeWAFDomain(m[1])
	}
	if m := urlHostRe.FindStringSubmatch(text); len(m) >= 2 {
		return NormalizeWAFDomain(m[1])
	}
	return ""
}

// IsWAFAlarm reports whether the alarm text is actually about WAF disposition
// (a rule naming WAF, an attack, or a block). Synthetic probes (云拨测) are
// whitelisted at the WAF edge and can never be blocked, so a dial-test alarm
// must NOT be anchored on WAF facts — doing so masks the real backend signal
// (a deployment, a readiness failure) that the probe actually caught.
func IsWAFAlarm(text string) bool {
	if strings.Contains(strings.ToUpper(text), "WAF") {
		return true
	}
	for _, kw := range []string{"Web攻击", "CC攻击", "攻击拦截", "拦截突增"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// ExtractBusinessUnitToken returns the business-unit token of the host named
// in alarm text ("www.jlc-smt.com" -> "smt"). k8s namespaces and service names
// are prefixed by that BU ("prod-smt", "smt-search-system"), so the token is
// the relevance anchor for a domain-scoped alarm whose real cause is a cluster
// workload (a dial-test failure, not WAF).
func ExtractBusinessUnitToken(text string) string {
	domain := ExtractWAFDomain(text)
	if domain == "" {
		return ""
	}
	labels := strings.Split(domain, ".")
	var label string
	for i := len(labels) - 1; i >= 0; i-- {
		l := strings.ToLower(labels[i])
		switch l {
		case "com", "cn", "net", "org", "io", "co", "com.cn", "co.cn":
			continue // TLD
		case "www", "api", "m", "app", "web", "static", "img", "test", "dev", "uat":
			continue // subdomain / env prefix
		}
		label = l
		break
	}
	if label == "" {
		return ""
	}
	segs := strings.Split(label, "-")
	token := segs[len(segs)-1]
	if len(token) < 2 {
		return ""
	}
	return token
}

// NormalizeWAFDomain strips WatchAlert naming suffixes (-waf/-access/-log).
// The domain itself is kept verbatim (including "www.") — subdomains are
// distinct protected objects and must not be blurred together.
func NormalizeWAFDomain(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	for _, suffix := range []string{"-waf", "-access", "-log"} {
		if strings.HasSuffix(d, suffix) {
			d = strings.TrimSuffix(d, suffix)
			break
		}
	}
	return d
}
