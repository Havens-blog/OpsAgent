package interfaces

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Havens-blog/OpsAgent/internal/llm"
)

// usageKey identifies a tenant/user bucket for in-memory usage aggregation.
type usageKey struct {
	TenantID int64
	UserID   int64
}

// usageBucket accumulates token usage and call counts for one tenant/user pair.
type usageBucket struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	LLMCalls         int64 `json:"llm_calls"`
	ToolCalls        int64 `json:"tool_calls"`
}

// usageMetrics is a thread-safe usage counter. When path is non-empty, every
// mutation also atomically rewrites a snapshot to disk so counts survive a
// restart. No quotas or limits are enforced here beyond the coarse tenant
// token check done at the HTTP request entry points.
type usageMetrics struct {
	mu      sync.Mutex
	buckets map[usageKey]*usageBucket
	path    string
}

// persistedUsage is the on-disk snapshot shape. Bucket keys are
// "<tenantID>:<userID>".
type persistedUsage struct {
	Buckets map[string]*usageBucket `json:"buckets"`
}

func newUsageMetrics() *usageMetrics {
	return &usageMetrics{buckets: make(map[usageKey]*usageBucket)}
}

// newFileBackedUsageMetrics loads the snapshot at path, or starts empty when
// it does not exist. An empty path or a corrupt/unreadable file returns an
// error.
func newFileBackedUsageMetrics(path string) (*usageMetrics, error) {
	if path == "" {
		return nil, fmt.Errorf("usage metrics path is required")
	}
	m := &usageMetrics{path: path, buckets: make(map[usageKey]*usageBucket)}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, nil
		}
		return nil, fmt.Errorf("read usage metrics %s: %w", path, err)
	}

	var snapshot persistedUsage
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("parse usage metrics %s: %w", path, err)
	}
	for k, b := range snapshot.Buckets {
		if b == nil {
			continue
		}
		tid, uid, err := parseUsageKey(k)
		if err != nil {
			return nil, fmt.Errorf("parse usage metrics key %q: %w", k, err)
		}
		m.buckets[usageKey{TenantID: tid, UserID: uid}] = b
	}
	return m, nil
}

func parseUsageKey(k string) (int64, int64, error) {
	parts := strings.SplitN(k, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid key")
	}
	tid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid tenant id: %w", err)
	}
	uid, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid user id: %w", err)
	}
	return tid, uid, nil
}

// persistLocked writes the current buckets to a temp file in the same directory
// and atomically renames it over the target. Callers must hold m.mu.
func (m *usageMetrics) persistLocked() error {
	if m.path == "" {
		return nil
	}
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create usage metrics dir: %w", err)
	}

	buckets := make(map[string]*usageBucket, len(m.buckets))
	for k, b := range m.buckets {
		buckets[fmt.Sprintf("%d:%d", k.TenantID, k.UserID)] = b
	}
	data, err := json.MarshalIndent(persistedUsage{Buckets: buckets}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode usage metrics: %w", err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(m.path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create usage metrics temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write usage metrics temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close usage metrics temp file: %w", err)
	}
	if err := os.Rename(tmpName, m.path); err != nil {
		return fmt.Errorf("replace usage metrics: %w", err)
	}
	return nil
}

func (m *usageMetrics) addLLM(tenantID, userID int64, u llm.Usage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	b := m.bucket(tenantID, userID)
	b.PromptTokens += int64(u.PromptTokens)
	b.CompletionTokens += int64(u.CompletionTokens)
	b.TotalTokens += int64(u.TotalTokens)
	b.LLMCalls++
	_ = m.persistLocked()
}

func (m *usageMetrics) addTool(tenantID, userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.bucket(tenantID, userID).ToolCalls++
	_ = m.persistLocked()
}

func (m *usageMetrics) tenantAggregate(tenantID int64) usageBucket {
	m.mu.Lock()
	defer m.mu.Unlock()

	var agg usageBucket
	for k, b := range m.buckets {
		if k.TenantID != tenantID {
			continue
		}
		agg.PromptTokens += b.PromptTokens
		agg.CompletionTokens += b.CompletionTokens
		agg.TotalTokens += b.TotalTokens
		agg.LLMCalls += b.LLMCalls
		agg.ToolCalls += b.ToolCalls
	}
	return agg
}

// tenantTokenTotal returns the summed total tokens consumed by a tenant.
func (m *usageMetrics) tenantTokenTotal(tenantID int64) int64 {
	return m.tenantAggregate(tenantID).TotalTokens
}

func (m *usageMetrics) userAggregate(tenantID, userID int64) usageBucket {
	m.mu.Lock()
	defer m.mu.Unlock()

	if b := m.buckets[usageKey{TenantID: tenantID, UserID: userID}]; b != nil {
		return *b
	}
	return usageBucket{}
}

// bucket returns the bucket for a tenant/user pair, creating it on demand.
// Callers must hold m.mu.
func (m *usageMetrics) bucket(tenantID, userID int64) *usageBucket {
	k := usageKey{TenantID: tenantID, UserID: userID}
	b, ok := m.buckets[k]
	if !ok {
		b = &usageBucket{}
		m.buckets[k] = b
	}
	return b
}
