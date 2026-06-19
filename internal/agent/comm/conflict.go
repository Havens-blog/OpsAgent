// Package comm provides conflict resolution strategies for multi-agent collaboration.
// When agents disagree about root cause conclusions, this module resolves the
// conflict through voting, confidence weighting, or evidence-based arbitration.
package comm

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ResolutionStrategy defines how a conflict is resolved.
type ResolutionStrategy int

const (
	// StrategyMajority resolves by simple majority vote (one agent = one vote)
	StrategyMajority ResolutionStrategy = iota
	// StrategyConfidenceWeighted weights each agent's vote by its self-reported confidence
	StrategyConfidenceWeighted
	// StrategyExpertPriority gives higher weight to domain-specialist agents
	StrategyExpertPriority
	// StrategyEvidenceStrength weights votes by the amount of supporting evidence
	StrategyEvidenceStrength
)

// VotePosition represents an agent's stance on a hypothesis.
type VotePosition string

const (
	VotePositionSupport   VotePosition = "support"
	VotePositionReject    VotePosition = "reject"
	VotePositionAbstain   VotePosition = "abstain"
	VotePositionInsufficient VotePosition = "insufficient_evidence"
)

// HypothesisVote represents a single agent's vote on a root cause hypothesis.
type HypothesisVote struct {
	AgentID      string       `json:"agent_id"`
	AgentType    string       `json:"agent_type"` // log_analyst, monitor, inspector, coordinator
	HypothesisID string       `json:"hypothesis_id"`
	Position     VotePosition `json:"position"`
	Confidence   float64      `json:"confidence"` // 0-1
	Reasoning    string       `json:"reasoning"`
	EvidenceKeys []string     `json:"evidence_keys,omitempty"` // Supporting evidence
	VotedAt      time.Time    `json:"voted_at"`
}

// VotingSession represents a voting session for resolving a conflict.
type VotingSession struct {
	ID            string            `json:"id"`
	IncidentID    string            `json:"incident_id"`
	Hypotheses    []string          `json:"hypotheses"` // Candidate hypothesis IDs
	Votes         []HypothesisVote  `json:"votes"`
	Strategy      ResolutionStrategy `json:"strategy"`
	StartedAt     time.Time         `json:"started_at"`
	Deadline      time.Time         `json:"deadline"`
	ClosedAt      time.Time         `json:"closed_at,omitempty"`
	mu            sync.Mutex
	closed        bool
}

// NewVotingSession creates a new voting session.
func NewVotingSession(id, incidentID string, hypotheses []string, strategy ResolutionStrategy, duration time.Duration) *VotingSession {
	return &VotingSession{
		ID:         id,
		IncidentID: incidentID,
		Hypotheses: hypotheses,
		Strategy:   strategy,
		StartedAt:  time.Now(),
		Deadline:   time.Now().Add(duration),
	}
}

// CastVote records an agent's vote.
func (s *VotingSession) CastVote(vote HypothesisVote) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("voting session %s is closed", s.ID)
	}

	if time.Now().After(s.Deadline) {
		return fmt.Errorf("voting session %s has expired", s.ID)
	}

	if vote.VotedAt.IsZero() {
		vote.VotedAt = time.Now()
	}

	// Check for duplicate vote (same agent, same hypothesis)
	for _, existing := range s.Votes {
		if existing.AgentID == vote.AgentID && existing.HypothesisID == vote.HypothesisID {
			return fmt.Errorf("agent %s already voted on %s", vote.AgentID, vote.HypothesisID)
		}
	}

	s.Votes = append(s.Votes, vote)
	return nil
}

// Close closes the voting session.
func (s *VotingSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.ClosedAt = time.Now()
}

// IsExpired checks if the voting session has expired.
func (s *VotingSession) IsExpired() bool {
	return time.Now().After(s.Deadline)
}

// ConflictResolution represents the outcome of conflict resolution.
type ConflictResolution struct {
	SessionID         string             `json:"session_id"`
	IncidentID        string             `json:"incident_id"`
	WinningHypothesis string             `json:"winning_hypothesis"`
	Strategy          ResolutionStrategy `json:"strategy"`
	Confidence        float64            `json:"confidence"` // Confidence in the resolution
	ScoreByHypothesis map[string]float64 `json:"score_by_hypothesis"`
	VoteCount         int                `json:"vote_count"`
	DissentCount      int                `json:"dissent_count"`
	Reasoning         string             `json:"reasoning"`
	Unresolved        bool               `json:"unresolved"` // True if no clear winner
	ResolvedAt        time.Time          `json:"resolved_at"`
}

// ConflictResolver resolves conflicts between agents using configurable strategies.
type ConflictResolver struct {
	store          *EvidenceStore
	expertWeights  map[string]float64 // agentType -> weight
	quorumFraction float64            // fraction of votes needed for a valid resolution
}

// ConflictResolverOption configures a ConflictResolver.
type ConflictResolverOption func(*ConflictResolver)

// WithExpertWeights sets the weight for each agent type in expert-priority mode.
func WithExpertWeights(weights map[string]float64) ConflictResolverOption {
	return func(r *ConflictResolver) {
		r.expertWeights = weights
	}
}

// WithQuorum sets the fraction of eligible voters required for a valid resolution.
func WithQuorum(fraction float64) ConflictResolverOption {
	return func(r *ConflictResolver) {
		r.quorumFraction = fraction
	}
}

// NewConflictResolver creates a new conflict resolver.
func NewConflictResolver(store *EvidenceStore, opts ...ConflictResolverOption) *ConflictResolver {
	r := &ConflictResolver{
		store: store,
		expertWeights: map[string]float64{
			"coordinator":  1.5,
			"monitor":      1.3,
			"log_analyst":  1.2,
			"inspector":    1.1,
			"default":      1.0,
		},
		quorumFraction: 0.5,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Resolve evaluates a voting session and produces a resolution.
func (r *ConflictResolver) Resolve(session *VotingSession) (*ConflictResolution, error) {
	if session == nil {
		return nil, fmt.Errorf("session is nil")
	}

	// Close the session if still open
	session.Close()

	resolution := &ConflictResolution{
		SessionID:         session.ID,
		IncidentID:        session.IncidentID,
		Strategy:          session.Strategy,
		ScoreByHypothesis: make(map[string]float64),
		ResolvedAt:        time.Now(),
	}

	// Tally votes by hypothesis using the configured strategy
	supporters := make(map[string][]string) // hypothesis -> supporting agents
	dissenters := make(map[string][]string) // hypothesis -> rejecting agents

	for _, vote := range session.Votes {
		weight := r.computeVoteWeight(session.Strategy, vote)

		switch vote.Position {
		case VotePositionSupport:
			resolution.ScoreByHypothesis[vote.HypothesisID] += weight
			supporters[vote.HypothesisID] = append(supporters[vote.HypothesisID], vote.AgentID)
		case VotePositionReject:
			resolution.ScoreByHypothesis[vote.HypothesisID] -= weight * 0.5 // Reject counts less than support
			dissenters[vote.HypothesisID] = append(dissenters[vote.HypothesisID], vote.AgentID)
		case VotePositionAbstain, VotePositionInsufficient:
			// No score change, but counts toward participation
		}

		resolution.VoteCount++
	}

	// Determine the winning hypothesis
	winner, winnerScore, runnerUpScore := r.findWinner(resolution.ScoreByHypothesis)
	resolution.WinningHypothesis = winner

	// Calculate confidence in the resolution
	resolution.Confidence = r.calculateResolutionConfidence(
		winnerScore, runnerUpScore, resolution.VoteCount, len(session.Hypotheses),
	)

	// Count dissent for the winner
	resolution.DissentCount = len(dissenters[winner])

	// Generate reasoning
	resolution.Reasoning = r.generateReasoning(session, resolution, winner)

	// Check if resolution is conclusive
	if winner == "" || resolution.Confidence < r.quorumFraction {
		resolution.Unresolved = true
	}

	return resolution, nil
}

// computeVoteWeight calculates the weight of a vote based on the session strategy.
func (r *ConflictResolver) computeVoteWeight(strategy ResolutionStrategy, vote HypothesisVote) float64 {
	switch strategy {
	case StrategyConfidenceWeighted:
		return vote.Confidence
	case StrategyExpertPriority:
		return r.expertWeightForType(vote.AgentType)
	case StrategyEvidenceStrength:
		w := float64(len(vote.EvidenceKeys))
		if w == 0 {
			w = 0.5
		}
		return w
	case StrategyMajority:
		fallthrough
	default:
		return 1.0
	}
}

// findWinner returns the hypothesis with the highest score, its score, and the runner-up's score.
func (r *ConflictResolver) findWinner(scores map[string]float64) (string, float64, float64) {
	if len(scores) == 0 {
		return "", 0, 0
	}

	type kv struct {
		key   string
		score float64
	}

	pairs := make([]kv, 0, len(scores))
	for k, v := range scores {
		pairs = append(pairs, kv{k, v})
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	winner := pairs[0]
	runnerUpScore := 0.0
	if len(pairs) > 1 {
		runnerUpScore = pairs[1].score
	}

	return winner.key, winner.score, runnerUpScore
}

// calculateResolutionConfidence calculates confidence in the resolution.
func (r *ConflictResolver) calculateResolutionConfidence(
	winnerScore, runnerUpScore float64,
	totalVotes, numHypotheses int,
) float64 {
	if totalVotes == 0 {
		return 0.0
	}

	// Base confidence from vote share
	baseConfidence := winnerScore / float64(totalVotes)
	if baseConfidence > 1 {
		baseConfidence = 1
	}

	// Margin of victory increases confidence
	margin := winnerScore - runnerUpScore
	marginBonus := margin / float64(totalVotes) * 0.3

	confidence := baseConfidence + marginBonus

	// Normalize to [0, 1]
	if confidence > 1 {
		confidence = 1
	}

	return confidence
}

// generateReasoning produces a human-readable explanation of the resolution.
func (r *ConflictResolver) generateReasoning(
	session *VotingSession,
	resolution *ConflictResolution,
	winner string,
) string {
	if winner == "" {
		return fmt.Sprintf("No hypothesis received support votes; incident %s unresolved",
			session.IncidentID)
	}

	supportCount := 0
	rejectCount := 0
	for _, vote := range session.Votes {
		if vote.HypothesisID == winner {
			if vote.Position == VotePositionSupport {
				supportCount++
			} else if vote.Position == VotePositionReject {
				rejectCount++
			}
		}
	}

	strategyName := strategyName(resolution.Strategy)

	return fmt.Sprintf("Hypothesis %s won under %s strategy with %d supporting and %d rejecting votes "+
		"(confidence: %.0f%%)",
		winner, strategyName, supportCount, rejectCount, resolution.Confidence*100)
}

// strategyName returns a human-readable name for a strategy.
func strategyName(s ResolutionStrategy) string {
	switch s {
	case StrategyMajority:
		return "majority vote"
	case StrategyConfidenceWeighted:
		return "confidence-weighted"
	case StrategyExpertPriority:
		return "expert-priority"
	case StrategyEvidenceStrength:
		return "evidence-strength"
	default:
		return "unknown"
	}
}

// ResolveByMajority tallies votes as simple majority (one agent = one vote).
func (r *ConflictResolver) ResolveByMajority(votes []HypothesisVote) (string, float64) {
	support := make(map[string]int)
	reject := make(map[string]int)

	for _, v := range votes {
		switch v.Position {
		case VotePositionSupport:
			support[v.HypothesisID]++
		case VotePositionReject:
			reject[v.HypothesisID]++
		}
	}

	bestHypothesis := ""
	bestScore := 0
	for h, s := range support {
		score := s - reject[h]
		if score > bestScore {
			bestScore = score
			bestHypothesis = h
		}
	}

	if len(votes) == 0 {
		return bestHypothesis, 0
	}

	confidence := float64(support[bestHypothesis]) / float64(len(votes))
	return bestHypothesis, confidence
}

// ResolveByConfidenceWeighted tallies votes weighted by each agent's confidence.
func (r *ConflictResolver) ResolveByConfidenceWeighted(votes []HypothesisVote) (string, float64) {
	support := make(map[string]float64)
	totalWeight := 0.0

	for _, v := range votes {
		weight := v.Confidence
		if v.Position == VotePositionSupport {
			support[v.HypothesisID] += weight
		} else if v.Position == VotePositionReject {
			support[v.HypothesisID] -= weight * 0.5
		}
		totalWeight += weight
	}

	bestHypothesis := ""
	bestScore := 0.0
	for h, s := range support {
		if s > bestScore {
			bestScore = s
			bestHypothesis = h
		}
	}

	if totalWeight == 0 {
		return bestHypothesis, 0
	}

	confidence := support[bestHypothesis] / totalWeight
	if confidence < 0 {
		confidence = 0
	}
	return bestHypothesis, confidence
}

// ResolveByExpertPriority tallies votes weighted by agent-type expertise.
func (r *ConflictResolver) ResolveByExpertPriority(votes []HypothesisVote) (string, float64) {
	support := make(map[string]float64)
	totalWeight := 0.0

	for _, v := range votes {
		weight := r.expertWeightForType(v.AgentType)
		if v.Position == VotePositionSupport {
			support[v.HypothesisID] += weight
		} else if v.Position == VotePositionReject {
			support[v.HypothesisID] -= weight * 0.5
		}
		totalWeight += weight
	}

	bestHypothesis := ""
	bestScore := 0.0
	for h, s := range support {
		if s > bestScore {
			bestScore = s
			bestHypothesis = h
		}
	}

	if totalWeight == 0 {
		return bestHypothesis, 0
	}

	confidence := support[bestHypothesis] / totalWeight
	if confidence < 0 {
		confidence = 0
	}
	return bestHypothesis, confidence
}

// expertWeightForType returns the weight for an agent type.
func (r *ConflictResolver) expertWeightForType(agentType string) float64 {
	if w, ok := r.expertWeights[agentType]; ok {
		return w
	}
	return r.expertWeights["default"]
}

// ResolveByEvidenceStrength tallies votes weighted by the amount of supporting evidence.
func (r *ConflictResolver) ResolveByEvidenceStrength(votes []HypothesisVote) (string, float64) {
	support := make(map[string]float64)
	totalWeight := 0.0

	for _, v := range votes {
		weight := float64(len(v.EvidenceKeys))
		if weight == 0 && v.Position == VotePositionSupport {
			weight = 0.5 // Minimal weight for unsupported votes
		}
		if v.Position == VotePositionSupport {
			support[v.HypothesisID] += weight
		} else if v.Position == VotePositionReject {
			support[v.HypothesisID] -= weight * 0.5
		}
		totalWeight += weight
	}

	bestHypothesis := ""
	bestScore := 0.0
	for h, s := range support {
		if s > bestScore {
			bestScore = s
			bestHypothesis = h
		}
	}

	if totalWeight == 0 {
		return bestHypothesis, 0
	}

	confidence := support[bestHypothesis] / totalWeight
	if confidence < 0 {
		confidence = 0
	}
	return bestHypothesis, confidence
}

// DistributedVoting coordinates a multi-agent vote over the broker.
type DistributedVoting struct {
	broker   *Broker
	resolver *ConflictResolver
	agentID  string
	sessions sync.Map // sessionID -> *VotingSession
}

// NewDistributedVoting creates a new distributed voting coordinator.
func NewDistributedVoting(broker *Broker, resolver *ConflictResolver, agentID string) *DistributedVoting {
	return &DistributedVoting{
		broker:   broker,
		resolver: resolver,
		agentID:  agentID,
	}
}

// StartSession initiates a voting session and invites agents to vote.
func (d *DistributedVoting) StartSession(
	ctx context.Context,
	sessionID, incidentID string,
	hypotheses []string,
	invitees []string,
	strategy ResolutionStrategy,
	duration time.Duration,
) error {
	session := NewVotingSession(sessionID, incidentID, hypotheses, strategy, duration)
	d.sessions.Store(sessionID, session)

	// Invite each agent to vote
	for _, agent := range invitees {
		req := &Message{
			From:    d.agentID,
			To:      agent,
			Type:    MessageTypeVote,
			Subject: fmt.Sprintf("Vote requested for session %s", sessionID),
			Payload: map[string]interface{}{
				"session_id":   sessionID,
				"incident_id":  incidentID,
				"hypotheses":   hypotheses,
				"deadline":     session.Deadline,
			},
			Priority:          PriorityHigh,
			DeliveryGuarantee: DeliveryAtLeastOnce,
		}
		_ = d.broker.Send(ctx, req)
	}

	return nil
}

// SubmitVote submits a vote to the local session and notifies the initiator.
func (d *DistributedVoting) SubmitVote(ctx context.Context, vote HypothesisVote, initiatorID string) error {
	// Store vote in local session if we own it
	if raw, ok := d.sessions.Load(vote.HypothesisID); ok {
		if session, ok := raw.(*VotingSession); ok {
			_ = session.CastVote(vote)
		}
	}

	// Send vote to initiator
	msg := &Message{
		From:    d.agentID,
		To:      initiatorID,
		Type:    MessageTypeVote,
		Subject: fmt.Sprintf("Vote from %s", d.agentID),
		Payload: vote,
		Priority:          PriorityNormal,
		DeliveryGuarantee: DeliveryAtLeastOnce,
	}

	return d.broker.Send(ctx, msg)
}

// CollectVote collects an incoming vote into a session.
func (d *DistributedVoting) CollectVote(vote HypothesisVote) error {
	// Find the session by incident or a mapping; here we expect the vote to
	// carry enough context. In practice the broker message carries session_id.
	raw, ok := d.sessions.Load(voteSessionKey(vote))
	if !ok {
		return fmt.Errorf("no session for vote")
	}

	session, ok := raw.(*VotingSession)
	if !ok {
		return fmt.Errorf("invalid session type")
	}

	return session.CastVote(vote)
}

// voteSessionKey derives a lookup key for a vote (placeholder: uses hypothesis ID).
func voteSessionKey(vote HypothesisVote) string {
	return vote.HypothesisID
}

// Finalize resolves a session and returns the resolution.
func (d *DistributedVoting) Finalize(sessionID string) (*ConflictResolution, error) {
	raw, ok := d.sessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	session, ok := raw.(*VotingSession)
	if !ok {
		return nil, fmt.Errorf("invalid session type")
	}

	return d.resolver.Resolve(session)
}

// GetSession returns a voting session by ID.
func (d *DistributedVoting) GetSession(sessionID string) (*VotingSession, bool) {
	raw, ok := d.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	session, ok := raw.(*VotingSession)
	return session, ok
}
