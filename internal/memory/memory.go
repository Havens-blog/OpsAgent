// Package memory stores long-term operational fault memory. Only stable,
// human-confirmed, service-scoped facts pass the gate; conversation-local or
// unconfirmed facts are rejected so transient context never pollutes the
// durable store. Facts are tenant-scoped and can carry a service version and
// an expiry time.
package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultMinConfidence 是写入长期记忆的最低置信度
const DefaultMinConfidence = 0.5

// FactScope describes the lifetime/authority of a fact.
type FactScope string

const (
	ScopeService FactScope = "service" // stable, descriptive service-level fact
	ScopeUser    FactScope = "user"    // user-level preference
	ScopeSession FactScope = "session" // conversation-local, not durable
)

// FactKind distinguishes the two fault-memory shapes OpsAgent records.
type FactKind string

const (
	FactRootCause FactKind = "root_cause"
	FactFragility FactKind = "fragility"
)

// Fact 是一条人工确认过的长期事实
type Fact struct {
	Kind        FactKind  `json:"kind"`
	Scope       FactScope `json:"scope"`
	Service     string    `json:"service"`
	Content     string    `json:"content"`
	Confidence  float64   `json:"confidence"`
	TenantID    int64     `json:"tenant_id"`
	Version     string    `json:"version,omitempty"`
	ConfirmedBy string    `json:"confirmed_by"`
	// ConfirmedByUID 是服务端认证主体的数字 ID，用于追溯真实确认者；
	// 客户端自报的 confirmed_by 字符串不能替代它。
	ConfirmedByUID int64     `json:"confirmed_by_uid,omitempty"`
	ConfirmedAt    time.Time `json:"confirmed_at"`
	// ExpiresAt 零值表示长期有效；过期后检索不再返回
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// key 按租户、版本与内容去重：不同租户或不同版本的相同结论是不同事实
func (f Fact) key() string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s",
		f.TenantID, f.Kind, f.Service, f.Version, strings.TrimSpace(f.Content))
}

// ErrScopeGate 表示事实被作用域门槛拒绝（输入问题，可映射为 4xx）。
// 与持久化失败（系统故障，应映射为 5xx）区分，避免误导客户端。
var ErrScopeGate = errors.New("fact rejected by scope gate")

// scopeGate 只有「服务级 + 内容完整 + 置信度达标 + 有人工确认者」的事实才能入库
func scopeGate(f Fact, minConfidence float64) bool {
	if f.Scope != ScopeService {
		return false
	}
	if strings.TrimSpace(f.Service) == "" || strings.TrimSpace(f.Content) == "" {
		return false
	}
	if f.Confidence < minConfidence {
		return false
	}
	if strings.TrimSpace(f.ConfirmedBy) == "" {
		return false
	}
	return true
}

// Query 是一次检索的过滤条件；Version 非空时精确匹配
type Query struct {
	TenantID int64
	Service  string
	Version  string
	// Now 为零时取当前时间，用于过滤已过期事实
	Now time.Time
}

// Store is a thread-safe, deduplicated fault memory. path 非空时变更落盘
type Store struct {
	mu            sync.Mutex
	path          string
	facts         map[string]Fact
	minConfidence float64
	// persistFn 是可注入的持久化函数（nil 时用 persistLocked），
	// 用于测试模拟落盘故障以验证内存回滚。
	persistFn func() error
}

// New creates an empty store requiring at least minConfidence to persist a fact.
func New(minConfidence float64) *Store {
	return &Store{facts: make(map[string]Fact), minConfidence: minConfidence}
}

// NewFileBacked loads the snapshot at path, or starts empty when it does not
// exist. A corrupt or unreadable file returns an error.
func NewFileBacked(minConfidence float64, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("fault memory path is required")
	}
	s := New(minConfidence)
	s.path = path
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read fault memory %s: %w", s.path, err)
	}
	var snap struct {
		Facts map[string]Fact `json:"facts"`
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("parse fault memory %s: %w", s.path, err)
	}
	for k, f := range snap.Facts {
		s.facts[k] = f
	}
	return nil
}

// persistLocked 写临时文件后原子替换，调用方须持有锁
func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create fault memory dir: %w", err)
	}
	data, err := json.MarshalIndent(struct {
		Facts map[string]Fact `json:"facts"`
	}{Facts: s.facts}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode fault memory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create fault memory temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write fault memory temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close fault memory temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace fault memory: %w", err)
	}
	return nil
}

// Record 把一条人工确认的事实写入长期记忆。ConfirmedBy 为空、非服务级、
// 内容不完整或置信度不足都会被拒绝；重复确认按幂等处理（不重复存储）。
// 落盘失败时回滚内存写入，避免留下"看似已存储、重启后丢失"的伪幂等状态。
func (s *Store) Record(f Fact) (bool, error) {
	if !scopeGate(f, s.minConfidence) {
		return false, ErrScopeGate
	}
	if f.ConfirmedAt.IsZero() {
		f.ConfirmedAt = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.facts[f.key()]; exists {
		return false, nil
	}
	s.facts[f.key()] = f
	persist := s.persistLocked
	if s.persistFn != nil {
		persist = s.persistFn
	}
	if err := persist(); err != nil {
		// 回滚内存写入：持久化失败不得让内存出现已拒绝数据，
		// 否则下一次重复提交会被误判为幂等成功。
		delete(s.facts, f.key())
		return false, err
	}
	return true, nil
}

// Query 按租户/服务/版本检索未过期的事实
func (s *Store) Query(q Query) []Fact {
	now := q.Now
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Fact, 0)
	for _, f := range s.facts {
		if f.TenantID != q.TenantID {
			continue
		}
		if q.Service != "" && f.Service != q.Service {
			continue
		}
		if q.Version != "" && f.Version != q.Version {
			continue
		}
		if !f.ExpiresAt.IsZero() && !f.ExpiresAt.After(now) {
			continue
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		return out[i].Content < out[j].Content
	})
	return out
}

// Count returns the number of stored facts.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.facts)
}
