package interfaces

import (
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
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	LLMCalls         int64
	ToolCalls        int64
}

// usageMetrics is a thread-safe, in-memory usage counter. It is observational
// only: no quotas or limits are enforced here, and values reset on restart.
type usageMetrics struct {
	mu      sync.Mutex
	buckets map[usageKey]*usageBucket
}

func newUsageMetrics() *usageMetrics {
	return &usageMetrics{buckets: make(map[usageKey]*usageBucket)}
}

func (m *usageMetrics) addLLM(tenantID, userID int64, u llm.Usage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	b := m.bucket(tenantID, userID)
	b.PromptTokens += int64(u.PromptTokens)
	b.CompletionTokens += int64(u.CompletionTokens)
	b.TotalTokens += int64(u.TotalTokens)
	b.LLMCalls++
}

func (m *usageMetrics) addTool(tenantID, userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.bucket(tenantID, userID).ToolCalls++
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
