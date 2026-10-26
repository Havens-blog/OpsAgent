// Package comm provides tests for the evidence sharing mechanism.
package comm

import (
	"context"
	"testing"
	"time"
)

func TestSharedEvidenceDedupKey(t *testing.T) {
	t.Run("Same Content Same Key", func(t *testing.T) {
		e1 := &SharedEvidence{
			Type:      EvidenceTypeLog,
			Title:     "Error spike in order-service",
			Service:   "order-service",
			Data:      "error count 500",
			StartTime: time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 6, 19, 11, 0, 0, 0, time.UTC),
		}

		e2 := &SharedEvidence{
			Type:      EvidenceTypeLog,
			Title:     "Error spike in order-service",
			Service:   "order-service",
			Data:      "error count 500",
			StartTime: time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 6, 19, 11, 0, 0, 0, time.UTC),
		}

		key1 := e1.ComputeDedupKey()
		key2 := e2.ComputeDedupKey()

		if key1 != key2 {
			t.Errorf("Same content should produce same dedup key: %s vs %s", key1, key2)
		}

		if key1 == "" {
			t.Error("Dedup key should not be empty")
		}
	})

	t.Run("Different Content Different Key", func(t *testing.T) {
		e1 := &SharedEvidence{
			Type:    EvidenceTypeLog,
			Title:   "Error A",
			Service: "svc-a",
			Data:    "data-a",
		}

		e2 := &SharedEvidence{
			Type:    EvidenceTypeLog,
			Title:   "Error B",
			Service: "svc-b",
			Data:    "data-b",
		}

		if e1.ComputeDedupKey() == e2.ComputeDedupKey() {
			t.Error("Different content should produce different dedup keys")
		}
	})

	t.Run("Precomputed Key Preserved", func(t *testing.T) {
		e := &SharedEvidence{
			DedupKey: "custom-key",
			Type:     EvidenceTypeLog,
		}

		if e.ComputeDedupKey() != "custom-key" {
			t.Error("Should preserve precomputed dedup key")
		}
	})
}

func TestEvidenceStoreAdd(t *testing.T) {
	t.Run("Add New Evidence", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyUnion)
		e := &SharedEvidence{
			Type:    EvidenceTypeLog,
			Title:   "Test evidence",
			Service: "svc-a",
			Data:    "data",
		}

		stored, added := store.Add(e)

		if !added {
			t.Error("Expected evidence to be newly added")
		}

		if stored == nil {
			t.Fatal("Expected stored evidence, got nil")
		}

		if store.Count() != 1 {
			t.Errorf("Expected count 1, got %d", store.Count())
		}
	})

	t.Run("Add Nil Evidence", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyUnion)
		_, added := store.Add(nil)

		if added {
			t.Error("Expected nil evidence not to be added")
		}
	})

	t.Run("Dedup on Same Content", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyUnion)

		e1 := &SharedEvidence{
			Type:    EvidenceTypeLog,
			Title:   "Same evidence",
			Service: "svc-a",
			Data:    "same data",
		}
		e2 := &SharedEvidence{
			Type:    EvidenceTypeLog,
			Title:   "Same evidence",
			Service: "svc-a",
			Data:    "same data",
		}

		store.Add(e1)
		_, added := store.Add(e2)

		if added {
			t.Error("Expected duplicate to be deduped, not newly added")
		}

		if store.Count() != 1 {
			t.Errorf("Expected count 1 after dedup, got %d", store.Count())
		}
	})
}

func TestMergeStrategies(t *testing.T) {
	baseTime := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)
	laterTime := time.Date(2026, 6, 19, 11, 0, 0, 0, time.UTC)

	t.Run("Union Strategy Unions Endorsements", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyUnion)

		e1 := &SharedEvidence{
			Type:        EvidenceTypeLog,
			Title:       "Test",
			Service:     "svc",
			Data:        "data",
			StartTime:   baseTime,
			EndTime:     laterTime,
			SourceAgent: "agent-a",
			Confidence:  0.5,
			EndorsedBy:  []string{"agent-a"},
		}
		e2 := &SharedEvidence{
			Type:        EvidenceTypeLog,
			Title:       "Test",
			Service:     "svc",
			Data:        "data",
			StartTime:   baseTime,
			EndTime:     laterTime,
			SourceAgent: "agent-b",
			Confidence:  0.9,
			EndorsedBy:  []string{"agent-b"},
		}

		store.Add(e1)
		merged, _ := store.Add(e2)

		// Union strategy unions endorsements
		if len(merged.EndorsedBy) < 2 {
			t.Errorf("Expected >= 2 endorsements after union, got %d", len(merged.EndorsedBy))
		}

		// Union strategy takes higher confidence
		if merged.Confidence != 0.9 {
			t.Errorf("Expected confidence 0.9 (higher), got %f", merged.Confidence)
		}
	})

	t.Run("HighestConfidence Strategy", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyHighestConfidence)

		e1 := &SharedEvidence{
			Type:        EvidenceTypeMetric,
			Title:       "Latency spike",
			Service:     "svc",
			Data:        "low-conf data",
			StartTime:   baseTime,
			EndTime:     laterTime,
			Confidence:  0.3,
		}
		e2 := &SharedEvidence{
			Type:        EvidenceTypeMetric,
			Title:       "Latency spike",
			Service:     "svc",
			Data:        "high-conf data",
			StartTime:   baseTime,
			EndTime:     laterTime,
			Confidence:  0.9,
		}

		store.Add(e1)
		merged, _ := store.Add(e2)

		if merged.Data != "high-conf data" {
			t.Errorf("Expected high-conf data to win, got %v", merged.Data)
		}
		if merged.Confidence != 0.9 {
			t.Errorf("Expected confidence 0.9, got %f", merged.Confidence)
		}
	})

	t.Run("Latest Strategy", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyLatest)

		e1 := &SharedEvidence{
			Type:        EvidenceTypeTrace,
			Title:       "Slow trace",
			Service:     "svc",
			Data:        "old data",
			StartTime:   baseTime,
			EndTime:     laterTime,
			Confidence:  0.9,
			CollectedAt: baseTime,
		}
		e2 := &SharedEvidence{
			Type:        EvidenceTypeTrace,
			Title:       "Slow trace",
			Service:     "svc",
			Data:        "new data",
			StartTime:   baseTime,
			EndTime:     laterTime,
			Confidence:  0.3,
			CollectedAt: laterTime,
		}

		store.Add(e1)
		merged, _ := store.Add(e2)

		if merged.Data != "new data" {
			t.Errorf("Expected newest data to win, got %v", merged.Data)
		}
	})
}

func TestEvidenceStoreEndorse(t *testing.T) {
	t.Run("Endorse Evidence", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyUnion)
		e := &SharedEvidence{
			Type:    EvidenceTypeLog,
			Title:   "Test",
			Service: "svc",
			Data:    "data",
		}

		stored, _ := store.Add(e)

		err := store.Endorse(stored.DedupKey, "agent-x", true)
		if err != nil {
			t.Fatalf("Failed to endorse: %v", err)
		}

		retrieved, ok := store.Get(stored.DedupKey)
		if !ok {
			t.Fatal("Failed to get evidence")
		}

		found := false
		for _, a := range retrieved.EndorsedBy {
			if a == "agent-x" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected agent-x in endorsers")
		}
	})

	t.Run("Contradict Evidence", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyUnion)
		e := &SharedEvidence{
			Type:    EvidenceTypeLog,
			Title:   "Test",
			Service: "svc",
			Data:    "data",
		}

		stored, _ := store.Add(e)

		err := store.Endorse(stored.DedupKey, "agent-y", false)
		if err != nil {
			t.Fatalf("Failed to contradict: %v", err)
		}

		retrieved, _ := store.Get(stored.DedupKey)

		found := false
		for _, a := range retrieved.ContradictedBy {
			if a == "agent-y" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected agent-y in contradictors")
		}
	})

	t.Run("Endorse Unknown Evidence Fails", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyUnion)
		err := store.Endorse("nonexistent", "agent-x", true)
		if err == nil {
			t.Error("Expected error endorsing unknown evidence")
		}
	})
}

func TestEvidenceStoreQueries(t *testing.T) {
	store := NewEvidenceStore(MergeStrategyUnion)

	// Add evidence of various types and incidents
	store.Add(&SharedEvidence{Type: EvidenceTypeLog, Title: "L1", IncidentID: "inc-1", Confidence: 0.8})
	store.Add(&SharedEvidence{Type: EvidenceTypeLog, Title: "L2", IncidentID: "inc-2", Confidence: 0.6})
	store.Add(&SharedEvidence{Type: EvidenceTypeMetric, Title: "M1", IncidentID: "inc-1", Confidence: 0.9})

	t.Run("GetByType", func(t *testing.T) {
		logs := store.GetByType(EvidenceTypeLog)
		if len(logs) != 2 {
			t.Errorf("Expected 2 logs, got %d", len(logs))
		}

		metrics := store.GetByType(EvidenceTypeMetric)
		if len(metrics) != 1 {
			t.Errorf("Expected 1 metric, got %d", len(metrics))
		}
	})

	t.Run("GetByIncident", func(t *testing.T) {
		inc1 := store.GetByIncident("inc-1")
		if len(inc1) != 2 {
			t.Errorf("Expected 2 evidence for inc-1, got %d", len(inc1))
		}

		inc2 := store.GetByIncident("inc-2")
		if len(inc2) != 1 {
			t.Errorf("Expected 1 evidence for inc-2, got %d", len(inc2))
		}
	})

	t.Run("GetAll", func(t *testing.T) {
		all := store.GetAll()
		if len(all) != 3 {
			t.Errorf("Expected 3 total, got %d", len(all))
		}
	})
}

func TestEvidenceStoreRemove(t *testing.T) {
	store := NewEvidenceStore(MergeStrategyUnion)
	e := &SharedEvidence{Type: EvidenceTypeLog, Title: "To remove", Service: "svc", Data: "data"}

	stored, _ := store.Add(e)

	t.Run("Remove Existing", func(t *testing.T) {
		removed := store.Remove(stored.DedupKey)
		if !removed {
			t.Error("Expected evidence to be removed")
		}
		if store.Count() != 0 {
			t.Errorf("Expected count 0, got %d", store.Count())
		}
	})

	t.Run("Remove Nonexistent", func(t *testing.T) {
		removed := store.Remove("nonexistent")
		if removed {
			t.Error("Expected false removing nonexistent")
		}
	})
}

func TestConflictTracker(t *testing.T) {
	t.Run("Record and Get Conflicts", func(t *testing.T) {
		tracker := NewConflictTracker()

		tracker.RecordDisagreement("ev-1", []string{"agent-a", "agent-b"}, []string{"agent-c"})

		conflicts := tracker.GetConflicts()
		if len(conflicts) != 1 {
			t.Errorf("Expected 1 conflict, got %d", len(conflicts))
		}

		if len(conflicts[0].Supporters) != 2 {
			t.Errorf("Expected 2 supporters, got %d", len(conflicts[0].Supporters))
		}
		if len(conflicts[0].Dissenters) != 1 {
			t.Errorf("Expected 1 dissenter, got %d", len(conflicts[0].Dissenters))
		}
	})

	t.Run("Resolve Conflict", func(t *testing.T) {
		tracker := NewConflictTracker()
		tracker.RecordDisagreement("ev-1", []string{"agent-a"}, []string{"agent-b"})

		err := tracker.ResolveConflict("ev-1", "supporters win")
		if err != nil {
			t.Fatalf("Failed to resolve: %v", err)
		}

		// Resolved conflicts should not appear in GetConflicts
		conflicts := tracker.GetConflicts()
		if len(conflicts) != 0 {
			t.Errorf("Expected 0 unresolved conflicts, got %d", len(conflicts))
		}
	})

	t.Run("Resolve Unknown Fails", func(t *testing.T) {
		tracker := NewConflictTracker()
		err := tracker.ResolveConflict("nonexistent", "x")
		if err == nil {
			t.Error("Expected error resolving unknown conflict")
		}
	})
}

func TestEvidenceSharer(t *testing.T) {
	broker := NewBroker()
	store := NewEvidenceStore(MergeStrategyUnion)
	sharer := NewEvidenceSharer(broker, store, "agent-a")

	t.Run("Share Adds to Local Store", func(t *testing.T) {
		e := &SharedEvidence{
			Type:    EvidenceTypeLog,
			Title:   "Shared evidence",
			Service: "svc",
			Data:    "data",
		}

		err := sharer.Share(context.Background(), e)
		if err != nil {
			t.Fatalf("Failed to share: %v", err)
		}

		if store.Count() != 1 {
			t.Errorf("Expected 1 in local store, got %d", store.Count())
		}

		if e.SourceAgent != "agent-a" {
			t.Errorf("Expected source agent agent-a, got %s", e.SourceAgent)
		}
	})

	t.Run("Share Broadcasts to Others", func(t *testing.T) {
		broker2 := NewBroker()
		broker2.Register("agent-a", "A", "test", 10)
		mailboxB, _ := broker2.Register("agent-b", "B", "test", 10)

		store2 := NewEvidenceStore(MergeStrategyUnion)
		sharer2 := NewEvidenceSharer(broker2, store2, "agent-a")

		e := &SharedEvidence{
			Type:    EvidenceTypeMetric,
			Title:   "Broadcast evidence",
			Service: "svc",
			Data:    "metric data",
		}

		err := sharer2.Share(context.Background(), e)
		if err != nil {
			t.Fatalf("Failed to share: %v", err)
		}

		time.Sleep(10 * time.Millisecond)

		// agent-b should receive the evidence share message
		if mailboxB.Len() != 1 {
			t.Errorf("Expected agent-b to receive 1 message, got %d", mailboxB.Len())
		}
	})

	t.Run("Receive Evidence Dedup", func(t *testing.T) {
		store3 := NewEvidenceStore(MergeStrategyUnion)
		sharer3 := NewEvidenceSharer(broker, store3, "agent-a")

		e := &SharedEvidence{
			Type:    EvidenceTypeLog,
			Title:   "Receive test",
			Service: "svc",
			Data:    "data",
		}

		msg := &Message{
			Type:    MessageTypeEvidenceShare,
			Payload: e,
		}

		_, added, err := sharer3.ReceiveEvidence(msg)
		if err != nil {
			t.Fatalf("Failed to receive: %v", err)
		}
		if !added {
			t.Error("Expected first receive to add")
		}

		// Receive duplicate
		_, added2, _ := sharer3.ReceiveEvidence(msg)
		if added2 {
			t.Error("Expected duplicate to be deduped")
		}

		if store3.Count() != 1 {
			t.Errorf("Expected count 1 after dedup, got %d", store3.Count())
		}
	})
}

func TestGetConsensusEvidence(t *testing.T) {
	broker := NewBroker()
	store := NewEvidenceStore(MergeStrategyUnion)
	sharer := NewEvidenceSharer(broker, store, "agent-a")

	// Add evidence with varying endorsements
	e1 := &SharedEvidence{Type: EvidenceTypeLog, Title: "E1", Service: "svc", Data: "d1"}
	e2 := &SharedEvidence{Type: EvidenceTypeLog, Title: "E2", Service: "svc", Data: "d2"}
	e3 := &SharedEvidence{Type: EvidenceTypeLog, Title: "E3", Service: "svc", Data: "d3"}

	stored1, _ := store.Add(e1)
	stored2, _ := store.Add(e2)
	store.Add(e3)

	// Endorse e1 by 3 agents, e2 by 1 agent
	store.Endorse(stored1.DedupKey, "a1", true)
	store.Endorse(stored1.DedupKey, "a2", true)
	store.Endorse(stored1.DedupKey, "a3", true)
	store.Endorse(stored2.DedupKey, "a1", true)

	consensus := sharer.GetConsensusEvidence(2)

	if len(consensus) != 1 {
		t.Fatalf("Expected 1 consensus evidence, got %d", len(consensus))
	}

	if consensus[0].Title != "E1" {
		t.Errorf("Expected E1 as top consensus, got %s", consensus[0].Title)
	}
}

func TestSummarize(t *testing.T) {
	store := NewEvidenceStore(MergeStrategyUnion)

	store.Add(&SharedEvidence{Type: EvidenceTypeLog, Title: "L1", IncidentID: "inc-1", Confidence: 0.9})
	store.Add(&SharedEvidence{Type: EvidenceTypeMetric, Title: "M1", IncidentID: "inc-1", Confidence: 0.7})
	store.Add(&SharedEvidence{Type: EvidenceTypeLog, Title: "L2", IncidentID: "inc-2", Confidence: 0.5})

	t.Run("Summarize By Incident", func(t *testing.T) {
		summary := store.Summarize("inc-1")

		if summary.IncidentID != "inc-1" {
			t.Errorf("Expected incident inc-1, got %s", summary.IncidentID)
		}
		if summary.TotalEvidence != 2 {
			t.Errorf("Expected 2 total evidence, got %d", summary.TotalEvidence)
		}
		if summary.ByType[EvidenceTypeLog] != 1 {
			t.Errorf("Expected 1 log, got %d", summary.ByType[EvidenceTypeLog])
		}
		if summary.ByType[EvidenceTypeMetric] != 1 {
			t.Errorf("Expected 1 metric, got %d", summary.ByType[EvidenceTypeMetric])
		}
		if len(summary.TopEvidence) == 0 {
			t.Error("Expected top evidence")
		}
		// Top should be highest confidence
		if summary.TopEvidence[0].Confidence != 0.9 {
			t.Errorf("Expected top confidence 0.9, got %f", summary.TopEvidence[0].Confidence)
		}
	})

	t.Run("Summarize All", func(t *testing.T) {
		summary := store.Summarize("")

		if summary.TotalEvidence != 3 {
			t.Errorf("Expected 3 total evidence, got %d", summary.TotalEvidence)
		}
	})
}

func TestEvidenceTypeValues(t *testing.T) {
	types := []EvidenceType{
		EvidenceTypeLog,
		EvidenceTypeMetric,
		EvidenceTypeTrace,
		EvidenceTypeChange,
		EvidenceTypeTopology,
		EvidenceTypeAlert,
		EvidenceTypeHypothesis,
	}

	seen := make(map[EvidenceType]bool)
	for _, et := range types {
		if et == "" {
			t.Error("Evidence type should not be empty")
		}
		if seen[et] {
			t.Errorf("Duplicate evidence type: %s", et)
		}
		seen[et] = true
	}
}

func TestMergeStrategyValues(t *testing.T) {
	strategies := []MergeStrategy{
		MergeStrategyUnion,
		MergeStrategyLatest,
		MergeStrategyHighestConfidence,
		MergeStrategyEndorsed,
	}

	seen := make(map[MergeStrategy]bool)
	for i, s := range strategies {
		if seen[s] {
			t.Errorf("Duplicate merge strategy at index %d", i)
		}
		seen[s] = true
	}
}
