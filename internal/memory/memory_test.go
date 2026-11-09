package memory

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func confirmedFact(service, content string, conf float64) Fact {
	return Fact{
		Kind:        FactRootCause,
		Scope:       ScopeService,
		Service:     service,
		Content:     content,
		Confidence:  conf,
		ConfirmedBy: "oncall-alice",
	}
}

func TestScopeGate(t *testing.T) {
	valid := confirmedFact("order", "连接池耗尽", 0.8)
	if !scopeGate(valid, 0.5) {
		t.Error("confirmed service-level fact should pass")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeSession, Service: "order", Content: "x", Confidence: 0.8, ConfirmedBy: "a"}, 0.5) {
		t.Error("session scope should be rejected")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeUser, Service: "order", Content: "x", Confidence: 0.8, ConfirmedBy: "a"}, 0.5) {
		t.Error("user scope should be rejected")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeService, Service: "", Content: "x", Confidence: 0.8, ConfirmedBy: "a"}, 0.5) {
		t.Error("empty service should be rejected")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeService, Service: "order", Content: " ", Confidence: 0.8, ConfirmedBy: "a"}, 0.5) {
		t.Error("empty content should be rejected")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeService, Service: "order", Content: "x", Confidence: 0.4, ConfirmedBy: "a"}, 0.5) {
		t.Error("low confidence should be rejected")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeService, Service: "order", Content: "x", Confidence: 0.8}, 0.5) {
		t.Error("missing human confirmation should be rejected")
	}
}

func TestRecordRequiresConfirmation(t *testing.T) {
	s := New(0.5)
	if _, err := s.Record(Fact{Kind: FactRootCause, Scope: ScopeService, Service: "order", Content: "x", Confidence: 0.9}); err == nil {
		t.Fatal("unconfirmed fact must be rejected")
	}
	if s.Count() != 0 {
		t.Errorf("count = %d, want 0", s.Count())
	}
}

func TestStoreRecordAndQuery(t *testing.T) {
	s := New(0.5)
	stored, err := s.Record(confirmedFact("order", "连接池耗尽", 0.8))
	if err != nil || !stored {
		t.Fatalf("record: stored=%v err=%v", stored, err)
	}
	if s.Count() != 1 {
		t.Fatalf("count = %d, want 1", s.Count())
	}

	// duplicate is not stored again
	stored, err = s.Record(confirmedFact("order", "连接池耗尽", 0.9))
	if err != nil || stored {
		t.Errorf("duplicate: stored=%v err=%v, want no-op", stored, err)
	}

	if _, err := s.Record(Fact{Kind: FactFragility, Scope: ScopeSession, Service: "order", Content: "临时抖动", Confidence: 0.9, ConfirmedBy: "a"}); err == nil {
		t.Error("session fact should be rejected")
	}

	got := s.Query(Query{Service: "order"})
	if len(got) != 1 || got[0].Content != "连接池耗尽" {
		t.Errorf("Query = %+v, want 1 root-cause fact", got)
	}
	if len(s.Query(Query{Service: "other"})) != 0 {
		t.Error("other service should have no facts")
	}
	if got[0].ConfirmedBy != "oncall-alice" || got[0].ConfirmedAt.IsZero() {
		t.Errorf("confirmation metadata missing: %+v", got[0])
	}
}

// TestQueryTenantIsolation 相同内容在不同租户各自独立存储与检索
func TestQueryTenantIsolation(t *testing.T) {
	s := New(0.5)
	f1 := confirmedFact("order", "连接池耗尽", 0.8)
	f1.TenantID = 1
	f2 := confirmedFact("order", "连接池耗尽", 0.8)
	f2.TenantID = 2
	if stored, err := s.Record(f1); err != nil || !stored {
		t.Fatalf("tenant1 record: stored=%v err=%v", stored, err)
	}
	if stored, err := s.Record(f2); err != nil || !stored {
		t.Fatalf("tenant2 record: stored=%v err=%v", stored, err)
	}
	if s.Count() != 2 {
		t.Fatalf("count = %d, want 2 (tenants are distinct)", s.Count())
	}
	if got := s.Query(Query{TenantID: 1, Service: "order"}); len(got) != 1 {
		t.Errorf("tenant1 query = %+v, want 1", got)
	}
	if got := s.Query(Query{TenantID: 2, Service: "order"}); len(got) != 1 {
		t.Errorf("tenant2 query = %+v, want 1", got)
	}
}

// TestQueryVersionFilter 同一服务不同版本是不同事实，可按版本检索
func TestQueryVersionFilter(t *testing.T) {
	s := New(0.5)
	v1 := confirmedFact("order", "连接池耗尽", 0.8)
	v1.Version = "1.0"
	v2 := confirmedFact("order", "连接池耗尽", 0.8)
	v2.Version = "2.0"
	_, _ = s.Record(v1)
	_, _ = s.Record(v2)

	if s.Count() != 2 {
		t.Fatalf("count = %d, want 2 (different versions)", s.Count())
	}
	if got := s.Query(Query{Service: "order", Version: "1.0"}); len(got) != 1 {
		t.Errorf("v1 query = %+v, want 1", got)
	}
	if got := s.Query(Query{Service: "order"}); len(got) != 2 {
		t.Errorf("all-version query = %+v, want 2", got)
	}
}

// TestQueryExcludesExpired 过期事实不再返回，未过期照常返回
func TestQueryExcludesExpired(t *testing.T) {
	s := New(0.5)
	live := confirmedFact("order", "未过期", 0.8)
	dead := confirmedFact("order", "已过期", 0.8)
	dead.ExpiresAt = time.Now().Add(-time.Hour)
	_, _ = s.Record(live)
	_, _ = s.Record(dead)

	got := s.Query(Query{Service: "order"})
	if len(got) != 1 || got[0].Content != "未过期" {
		t.Errorf("Query = %+v, want only the live fact", got)
	}
}

// TestFileBackedPersistence 确认的事实落盘，重启后可检索且幂等不重复
func TestFileBackedPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")
	s, err := NewFileBacked(0.5, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f := confirmedFact("order", "连接池耗尽", 0.8)
	f.TenantID = 7
	if stored, err := s.Record(f); err != nil || !stored {
		t.Fatalf("record: stored=%v err=%v", stored, err)
	}

	reopened, err := NewFileBacked(0.5, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := reopened.Query(Query{TenantID: 7, Service: "order"})
	if len(got) != 1 || got[0].Content != "连接池耗尽" || got[0].ConfirmedBy != "oncall-alice" {
		t.Fatalf("query after restart = %+v, want the confirmed fact", got)
	}
	if stored, err := reopened.Record(f); err != nil || stored {
		t.Errorf("duplicate across restart: stored=%v err=%v", stored, err)
	}
}

// TestRecordRollsBackOnPersistFailure 落盘失败时回滚内存写入，不产生伪幂等
func TestRecordRollsBackOnPersistFailure(t *testing.T) {
	s := New(0.5)
	failErr := errors.New("disk full")
	s.persistFn = func() error { return failErr }

	f := confirmedFact("order", "连接池耗尽", 0.8)
	if _, err := s.Record(f); !errors.Is(err, failErr) {
		t.Fatalf("record err = %v, want injected persist failure", err)
	}
	// 内存查询不得看到已拒绝数据
	if s.Count() != 0 {
		t.Fatalf("count = %d, want 0 (failed persist must roll back)", s.Count())
	}
	if got := s.Query(Query{Service: "order"}); len(got) != 0 {
		t.Fatalf("query = %+v, want empty after rollback", got)
	}

	// 恢复存储后重试必须真实提交
	s.persistFn = nil
	if stored, err := s.Record(f); err != nil || !stored {
		t.Fatalf("retry: stored=%v err=%v, want real commit", stored, err)
	}
	if s.Count() != 1 {
		t.Fatalf("count after retry = %d, want 1", s.Count())
	}
}
