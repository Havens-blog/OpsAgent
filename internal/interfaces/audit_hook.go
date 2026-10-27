// Config-change audit hooks for the API layer.
//
// Reuses the existing observability audit infrastructure: events are
// observability.AuditEvent with EventType=config_change, persisted (when
// security.audit_log_path is set) through observability.FileAuditWriter so
// API-side records land in the same JSONL stream the agent already writes,
// with the same sensitive-data redaction. The in-memory ring exists because
// FileAuditWriter is write-only — the console's security page needs to read
// recent records back.
package interfaces

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/observability"
)

// auditEntry is one config-change record as the console renders it.
type auditEntry struct {
	Time      time.Time `json:"time"`
	User      string    `json:"user"`
	Operation string    `json:"operation"`
	IP        string    `json:"ip"`
	Result    string    `json:"result"` // success | failed
	Detail    string    `json:"detail,omitempty"`
}

// recentAuditLog is a bounded in-memory ring of audit entries (newest last).
type recentAuditLog struct {
	mu      sync.Mutex
	entries []auditEntry
	max     int
}

func newRecentAuditLog(max int) *recentAuditLog {
	return &recentAuditLog{max: max}
}

func (l *recentAuditLog) add(e auditEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	if len(l.entries) > l.max {
		l.entries = l.entries[len(l.entries)-l.max:]
	}
}

// recent returns up to n entries, newest first.
func (l *recentAuditLog) recent(n int) []auditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.entries) {
		n = len(l.entries)
	}
	out := make([]auditEntry, 0, n)
	for i := len(l.entries) - 1; i >= len(l.entries)-n; i-- {
		out = append(out, l.entries[i])
	}
	return out
}

// recordConfigChange records a config mutation: in-memory ring (read back by
// the security page) plus JSONL file append when audit_log_path is configured.
// Gated on security.enable_audit_log read at call time. Callers apply the
// pre-change gate by invoking this BEFORE SetSecurity for the security config
// itself, so the final "disable auditing" change is still captured.
func (s *APIServer) recordConfigChange(r *http.Request, operation string, opErr error) {
	sec := s.settings.ToSecurityConfig()
	if !sec.EnableAuditLog {
		return
	}

	user := "local"
	if s.eiamSessionKey != "" {
		user = EIAMUsername(r.Context())
		if user == "" {
			user = "unknown"
		}
	}

	entry := auditEntry{
		Time:      time.Now(),
		User:      user,
		Operation: operation,
		IP:        clientIP(r),
		Result:    "success",
	}
	if opErr != nil {
		entry.Result = "failed"
		entry.Detail = opErr.Error()
	}
	s.auditLog.add(entry)

	if sec.AuditLogPath == "" {
		return
	}
	// Best-effort file append. The writer is opened per event (append mode):
	// config changes are human-scale rare, and this survives runtime changes
	// to audit_log_path without lifecycle management.
	w, err := observability.NewFileAuditWriter(sec.AuditLogPath)
	if err != nil {
		s.logger.Warn("audit file writer unavailable",
			observability.String("path", sec.AuditLogPath), observability.Err(err))
		return
	}
	defer w.Close()
	_ = w.Write(&observability.AuditEvent{
		Timestamp: entry.Time,
		EventType: observability.EventConfigChange,
		Action:    entry.Operation,
		Result:    entry.Result,
		Error:     entry.Detail,
		Metadata:  map[string]interface{}{"user": entry.User, "ip": entry.IP},
	})
}

// clientIP resolves the caller IP: X-Forwarded-For (first hop) → X-Real-IP →
// RemoteAddr. The console is served behind nginx (:8888), which sets the
// forwarded headers.
func clientIP(r *http.Request) string {
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xf != "" {
		if i := strings.IndexByte(xf, ','); i >= 0 {
			xf = xf[:i]
		}
		return strings.TrimSpace(xf)
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
		return xr
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
