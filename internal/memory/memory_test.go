package memory

import "testing"

func TestScopeGate(t *testing.T) {
	valid := Fact{Kind: FactRootCause, Scope: ScopeService, Service: "order", Content: "连接池耗尽", Confidence: 0.8}
	if !scopeGate(valid, 0.5) {
		t.Error("service-level fact should pass")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeSession, Service: "order", Content: "x", Confidence: 0.8}, 0.5) {
		t.Error("session scope should be rejected")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeUser, Service: "order", Content: "x", Confidence: 0.8}, 0.5) {
		t.Error("user scope should be rejected")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeService, Service: "", Content: "x", Confidence: 0.8}, 0.5) {
		t.Error("empty service should be rejected")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeService, Service: "order", Content: " ", Confidence: 0.8}, 0.5) {
		t.Error("empty content should be rejected")
	}
	if scopeGate(Fact{Kind: FactRootCause, Scope: ScopeService, Service: "order", Content: "x", Confidence: 0.4}, 0.5) {
		t.Error("low confidence should be rejected")
	}
}

func TestStoreRecordAndQuery(t *testing.T) {
	s := New(0.5)
	stored, err := s.Record(Fact{Kind: FactRootCause, Scope: ScopeService, Service: "order", Content: "连接池耗尽", Confidence: 0.8})
	if err != nil || !stored {
		t.Fatalf("record: stored=%v err=%v", stored, err)
	}
	if s.Count() != 1 {
		t.Fatalf("count = %d, want 1", s.Count())
	}

	// duplicate is not stored again
	stored, err = s.Record(Fact{Kind: FactRootCause, Scope: ScopeService, Service: "order", Content: "连接池耗尽", Confidence: 0.9})
	if err != nil || stored {
		t.Errorf("duplicate: stored=%v err=%v, want no-op", stored, err)
	}

	if _, err := s.Record(Fact{Kind: FactFragility, Scope: ScopeSession, Service: "order", Content: "临时抖动", Confidence: 0.9}); err == nil {
		t.Error("session fact should be rejected")
	}

	got := s.ForService("order")
	if len(got) != 1 || got[0].Content != "连接池耗尽" {
		t.Errorf("ForService = %+v, want 1 root-cause fact", got)
	}
	if len(s.ForService("other")) != 0 {
		t.Error("other service should have no facts")
	}
}
