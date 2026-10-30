package models

// EdgeBackend is one backend behind the domain's SLB/ALB.
type EdgeBackend struct {
	Service  string `json:"service"`
	TargetIP string `json:"target_ip"`
	Port     int    `json:"port"`
}

// SLBEdgeFacts summarizes the domain's SLB/ALB access-log traffic in the
// window: total matched requests, the sampled 5xx count, and the resolved
// backends. For a dial-test "unavailable rate" alarm, a high 5xx ratio means
// the SLB->backend forwarding failed; a near-zero one means the SLB forwarded
// fine and the failure lived upstream (or at the probe edge).
type SLBEdgeFacts struct {
	Total      int64         `json:"total"`
	SampleSize int           `json:"sample_size"`
	Status5xx  int           `json:"status_5xx"`
	Backends   []EdgeBackend `json:"backends"`
}

// CDNEdgeFacts records the domain's CDN hop: the provider/CNAME/origin from the
// CMDB, plus the e-cam CDN access-log edge-5xx count. LogChecked marks a
// successful log query; Total == 0 then means either no traffic or — the common
// case for a domain whose CDN log collection is not configured — no data, which
// is stated honestly rather than passed off as "CDN healthy".
type CDNEdgeFacts struct {
	Provider   string `json:"provider"`    // e.g. aliyun
	CNAME      string `json:"cname"`       // CDN acceleration CNAME
	OriginHost string `json:"origin_host"` // CDN origin (usually the WAF CNAME)
	OriginType string `json:"origin_type"` // domain / oss
	// LogChecked marks that the CDN access-log query succeeded. It separates a
	// genuine "0 records" (collection off) from a query failure, so the report
	// never passes a failed lookup off as "CDN log not collected".
	LogChecked bool `json:"log_checked"`
	// Total is the federation-reported match count for the window; SampleSize is
	// how many entries were actually sampled to count 5xx below.
	Total      int64 `json:"total"`
	SampleSize int   `json:"sample_size"`
	Status5xx  int   `json:"status_5xx"` // CDN edge responses with status >= 500
}

// EdgeFacts carries window-scoped edge facts for a domain alarm. It is
// reference material only (never scored), so a dial-test "unavailable rate"
// report can show the full edge chain (CDN -> WAF -> SLB -> backend) and which
// hops actually have data, instead of leaving "edge suspicion" unsupported.
//
// WAF facts are deliberately NOT part of this: a synthetic probe (云拨测) is
// whitelisted at the WAF edge and can never be blocked, so WAF disposition in
// the window is uncorrelated with the probe's availability and would only
// mislead on-call into blaming the WAF again.
type EdgeFacts struct {
	Domain string        `json:"domain"`
	CDN    *CDNEdgeFacts `json:"cdn,omitempty"`
	SLB    *SLBEdgeFacts `json:"slb,omitempty"`
}
