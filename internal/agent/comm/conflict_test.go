// Package comm provides tests for conflict resolution strategies.
package comm

import (
	"context"
	"testing"
	"time"
)

func TestVotingSession(t *testing.T) {
	t.Run("CastVote and Close", func(t *testing.T) {
		session := NewVotingSession("s1", "inc-1", []string{"h1", "h2"}, StrategyMajority, 1*time.Minute)

		vote := HypothesisVote{
			AgentID:      "agent-a",
			AgentType:    "log_analyst",
			HypothesisID: "h1",
			Position:     VotePositionSupport,
			Confidence:   0.8,
		}

		err := session.CastVote(vote)
		if err != nil {
			t.Fatalf("Failed to cast vote: %v", err)
		}

		if len(session.Votes) != 1 {
			t.Errorf("Expected 1 vote, got %d", len(session.Votes))
		}

		session.Close()

		// Should not accept votes after close
		err = session.CastVote(HypothesisVote{
			AgentID:      "agent-b",
			HypothesisID: "h1",
			Position:     VotePositionSupport,
		})
		if err == nil {
			t.Error("Expected error voting on closed session")
		}
	})

	t.Run("Duplicate Vote Rejected", func(t *testing.T) {
		session := NewVotingSession("s1", "inc-1", []string{"h1"}, StrategyMajority, 1*time.Minute)

		vote := HypothesisVote{
			AgentID:      "agent-a",
			HypothesisID: "h1",
			Position:     VotePositionSupport,
		}

		_ = session.CastVote(vote)
		err := session.CastVote(vote)

		if err == nil {
			t.Error("Expected error on duplicate vote")
		}
	})

	t.Run("Expired Session Rejects Votes", func(t *testing.T) {
		session := NewVotingSession("s1", "inc-1", []string{"h1"}, StrategyMajority, -1*time.Minute)

		err := session.CastVote(HypothesisVote{
			AgentID:      "agent-a",
			HypothesisID: "h1",
			Position:     VotePositionSupport,
		})

		if err == nil {
			t.Error("Expected error voting on expired session")
		}

		if !session.IsExpired() {
			t.Error("Expected session to be expired")
		}
	})
}

func TestConflictResolverResolve(t *testing.T) {
	store := NewEvidenceStore(MergeStrategyUnion)
	resolver := NewConflictResolver(store)

	t.Run("Clear Winner Resolved", func(t *testing.T) {
		session := NewVotingSession("s1", "inc-1", []string{"h1", "h2"}, StrategyMajority, 1*time.Minute)

		_ = session.CastVote(HypothesisVote{AgentID: "a1", HypothesisID: "h1", Position: VotePositionSupport})
		_ = session.CastVote(HypothesisVote{AgentID: "a2", HypothesisID: "h1", Position: VotePositionSupport})
		_ = session.CastVote(HypothesisVote{AgentID: "a3", HypothesisID: "h2", Position: VotePositionSupport})

		resolution, err := resolver.Resolve(session)
		if err != nil {
			t.Fatalf("Failed to resolve: %v", err)
		}

		if resolution.WinningHypothesis != "h1" {
			t.Errorf("Expected winner h1, got %s", resolution.WinningHypothesis)
		}

		if resolution.Unresolved {
			t.Error("Expected resolution to be conclusive")
		}

		if resolution.Confidence <= 0 {
			t.Errorf("Expected positive confidence, got %f", resolution.Confidence)
		}
	})

	t.Run("No Votes Unresolved", func(t *testing.T) {
		session := NewVotingSession("s2", "inc-2", []string{"h1"}, StrategyMajority, 1*time.Minute)

		resolution, err := resolver.Resolve(session)
		if err != nil {
			t.Fatalf("Failed to resolve: %v", err)
		}

		if !resolution.Unresolved {
			t.Error("Expected unresolved with no votes")
		}
	})

	t.Run("Nil Session Fails", func(t *testing.T) {
		_, err := resolver.Resolve(nil)
		if err == nil {
			t.Error("Expected error resolving nil session")
		}
	})

	t.Run("Dissent Counted", func(t *testing.T) {
		session := NewVotingSession("s3", "inc-3", []string{"h1"}, StrategyMajority, 1*time.Minute)

		_ = session.CastVote(HypothesisVote{AgentID: "a1", HypothesisID: "h1", Position: VotePositionSupport})
		_ = session.CastVote(HypothesisVote{AgentID: "a2", HypothesisID: "h1", Position: VotePositionSupport})
		_ = session.CastVote(HypothesisVote{AgentID: "a3", HypothesisID: "h1", Position: VotePositionReject})

		resolution, _ := resolver.Resolve(session)

		if resolution.DissentCount != 1 {
			t.Errorf("Expected 1 dissenter, got %d", resolution.DissentCount)
		}
	})
}

func TestResolveByMajority(t *testing.T) {
	store := NewEvidenceStore(MergeStrategyUnion)
	resolver := NewConflictResolver(store)

	t.Run("Majority Wins", func(t *testing.T) {
		votes := []HypothesisVote{
			{AgentID: "a1", HypothesisID: "h1", Position: VotePositionSupport, Confidence: 0.5},
			{AgentID: "a2", HypothesisID: "h1", Position: VotePositionSupport, Confidence: 0.5},
			{AgentID: "a3", HypothesisID: "h2", Position: VotePositionSupport, Confidence: 0.5},
		}

		winner, confidence := resolver.ResolveByMajority(votes)

		if winner != "h1" {
			t.Errorf("Expected winner h1, got %s", winner)
		}

		if confidence <= 0 {
			t.Errorf("Expected positive confidence, got %f", confidence)
		}
	})

	t.Run("Rejects Reduce Score Below Threshold", func(t *testing.T) {
		votes := []HypothesisVote{
			{AgentID: "a1", HypothesisID: "h1", Position: VotePositionSupport},
			{AgentID: "a2", HypothesisID: "h1", Position: VotePositionReject},
			{AgentID: "a3", HypothesisID: "h1", Position: VotePositionReject},
		}

		winner, _ := resolver.ResolveByMajority(votes)

		// Net score for h1: 1 - 2 = -1. A hypothesis rejected more than
		// supported should not be declared the winner even if it is the
		// only candidate — the incident is effectively unresolved.
		if winner != "" {
			t.Errorf("Expected no winner when rejects outweigh support, got %s", winner)
		}
	})

	t.Run("Empty Votes", func(t *testing.T) {
		winner, confidence := resolver.ResolveByMajority([]HypothesisVote{})

		if winner != "" {
			t.Errorf("Expected empty winner, got %s", winner)
		}
		if confidence != 0 {
			t.Errorf("Expected zero confidence, got %f", confidence)
		}
	})
}

func TestResolveByConfidenceWeighted(t *testing.T) {
	store := NewEvidenceStore(MergeStrategyUnion)
	resolver := NewConflictResolver(store)

	t.Run("High Confidence Vote Wins", func(t *testing.T) {
		votes := []HypothesisVote{
			{AgentID: "a1", HypothesisID: "h1", Position: VotePositionSupport, Confidence: 0.9},
			{AgentID: "a2", HypothesisID: "h2", Position: VotePositionSupport, Confidence: 0.3},
			{AgentID: "a3", HypothesisID: "h2", Position: VotePositionSupport, Confidence: 0.3},
		}

		winner, _ := resolver.ResolveByConfidenceWeighted(votes)

		// h1 has weight 0.9; h2 has weight 0.6 total → h1 wins
		if winner != "h1" {
			t.Errorf("Expected h1 to win with high confidence, got %s", winner)
		}
	})
}

func TestResolveByExpertPriority(t *testing.T) {
	store := NewEvidenceStore(MergeStrategyUnion)
	resolver := NewConflictResolver(store, WithExpertWeights(map[string]float64{
		"coordinator": 2.0,
		"monitor":     1.5,
		"log_analyst": 1.0,
	}))

	t.Run("Expert Vote Outweighs Multiple Regular Votes", func(t *testing.T) {
		votes := []HypothesisVote{
			{AgentID: "a1", AgentType: "coordinator", HypothesisID: "h1", Position: VotePositionSupport, Confidence: 0.8},
			{AgentID: "a2", AgentType: "log_analyst", HypothesisID: "h2", Position: VotePositionSupport, Confidence: 0.8},
		}

		winner, _ := resolver.ResolveByExpertPriority(votes)

		// Coordinator weight 2.0 > log_analyst weight 1.0
		if winner != "h1" {
			t.Errorf("Expected h1 (coordinator) to win, got %s", winner)
		}
	})
}

func TestResolveByEvidenceStrength(t *testing.T) {
	store := NewEvidenceStore(MergeStrategyUnion)
	resolver := NewConflictResolver(store)

	t.Run("More Evidence Wins", func(t *testing.T) {
		votes := []HypothesisVote{
			{
				AgentID: "a1", HypothesisID: "h1", Position: VotePositionSupport,
				EvidenceKeys: []string{"e1", "e2", "e3", "e4"},
			},
			{
				AgentID: "a2", HypothesisID: "h2", Position: VotePositionSupport,
				EvidenceKeys: []string{"e5"},
			},
		}

		winner, _ := resolver.ResolveByEvidenceStrength(votes)

		// h1 has 4 evidence; h2 has 1 → h1 wins
		if winner != "h1" {
			t.Errorf("Expected h1 (more evidence) to win, got %s", winner)
		}
	})
}

func TestDistributedVoting(t *testing.T) {
	broker := NewBroker()
	store := NewEvidenceStore(MergeStrategyUnion)
	resolver := NewConflictResolver(store)

	coordinator := NewDistributedVoting(broker, resolver, "coordinator")

	broker.Register("coordinator", "Coordinator", "coordinator", 10)
	mailboxA, _ := broker.Register("agent-a", "LogAnalyst", "log_analyst", 10)
	mailboxB, _ := broker.Register("agent-b", "Monitor", "monitor", 10)

	t.Run("StartSession Invites Agents", func(t *testing.T) {
		err := coordinator.StartSession(
			context.Background(),
			"session-1",
			"inc-1",
			[]string{"h1", "h2"},
			[]string{"agent-a", "agent-b"},
			StrategyMajority,
			1*time.Minute,
		)
		if err != nil {
			t.Fatalf("Failed to start session: %v", err)
		}

		time.Sleep(10 * time.Millisecond)

		// Both agents should receive vote requests
		if mailboxA.Len() != 1 {
			t.Errorf("Expected agent-a to receive 1 invite, got %d", mailboxA.Len())
		}
		if mailboxB.Len() != 1 {
			t.Errorf("Expected agent-b to receive 1 invite, got %d", mailboxB.Len())
		}
	})

	t.Run("GetSession", func(t *testing.T) {
		session, ok := coordinator.GetSession("session-1")
		if !ok {
			t.Fatal("Expected session to exist")
		}

		if session.ID != "session-1" {
			t.Errorf("Expected session ID session-1, got %s", session.ID)
		}
	})

	t.Run("Finalize Unknown Session Fails", func(t *testing.T) {
		_, err := coordinator.Finalize("nonexistent")
		if err == nil {
			t.Error("Expected error finalizing unknown session")
		}
	})
}

func TestConflictResolutionStructure(t *testing.T) {
	t.Run("Resolution Fields", func(t *testing.T) {
		resolution := &ConflictResolution{
			SessionID:         "s1",
			IncidentID:        "inc-1",
			WinningHypothesis: "h1",
			Strategy:          StrategyMajority,
			Confidence:        0.85,
			VoteCount:         5,
			DissentCount:      1,
			Reasoning:         "h1 won by majority",
		}

		if resolution.WinningHypothesis == "" {
			t.Error("WinningHypothesis should not be empty")
		}
		if resolution.Confidence < 0 || resolution.Confidence > 1 {
			t.Errorf("Confidence should be between 0 and 1, got %f", resolution.Confidence)
		}
	})
}

func TestStrategyName(t *testing.T) {
	tests := []struct {
		strategy ResolutionStrategy
		expected string
	}{
		{StrategyMajority, "majority vote"},
		{StrategyConfidenceWeighted, "confidence-weighted"},
		{StrategyExpertPriority, "expert-priority"},
		{StrategyEvidenceStrength, "evidence-strength"},
	}

	for _, tt := range tests {
		name := strategyName(tt.strategy)
		if name != tt.expected {
			t.Errorf("Expected %s, got %s", tt.expected, name)
		}
	}
}

func TestVotePosition(t *testing.T) {
	t.Run("Position Values", func(t *testing.T) {
		positions := []VotePosition{
			VotePositionSupport,
			VotePositionReject,
			VotePositionAbstain,
			VotePositionInsufficient,
		}

		seen := make(map[VotePosition]bool)
		for _, p := range positions {
			if p == "" {
				t.Error("Position should not be empty")
			}
			if seen[p] {
				t.Errorf("Duplicate position: %s", p)
			}
			seen[p] = true
		}
	})
}

func TestConflictResolverOptions(t *testing.T) {
	t.Run("WithQuorum", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyUnion)
		resolver := NewConflictResolver(store, WithQuorum(0.8))

		if resolver.quorumFraction != 0.8 {
			t.Errorf("Expected quorum 0.8, got %f", resolver.quorumFraction)
		}
	})

	t.Run("WithExpertWeights", func(t *testing.T) {
		store := NewEvidenceStore(MergeStrategyUnion)
		resolver := NewConflictResolver(store, WithExpertWeights(map[string]float64{
			"custom_agent": 3.0,
		}))

		if resolver.expertWeightForType("custom_agent") != 3.0 {
			t.Error("Expected custom expert weight")
		}
	})
}

func TestExpertWeightForType(t *testing.T) {
	store := NewEvidenceStore(MergeStrategyUnion)
	resolver := NewConflictResolver(store)

	t.Run("Known Type", func(t *testing.T) {
		w := resolver.expertWeightForType("coordinator")
		if w != 1.5 {
			t.Errorf("Expected coordinator weight 1.5, got %f", w)
		}
	})

	t.Run("Unknown Type Uses Default", func(t *testing.T) {
		w := resolver.expertWeightForType("unknown_type")
		if w != 1.0 {
			t.Errorf("Expected default weight 1.0, got %f", w)
		}
	})
}
