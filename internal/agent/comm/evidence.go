// Package comm provides the evidence sharing mechanism for multi-agent collaboration.
// Agents collect evidence during root cause analysis and share it through the broker.
// This module handles deduplication and merge strategies for shared evidence.
package comm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// EvidenceType represents the type of evidence.
type EvidenceType string

const (
	EvidenceTypeLog      EvidenceType = "log"
	EvidenceTypeMetric   EvidenceType = "metric"
	EvidenceTypeTrace    EvidenceType = "trace"
	EvidenceTypeChange   EvidenceType = "change"
	EvidenceTypeTopology EvidenceType = "topology"
	EvidenceTypeAlert    EvidenceType = "alert"
	EvidenceTypeHypothesis EvidenceType = "hypothesis"
)

// SharedEvidence represents evidence collected and shared by an agent.
type SharedEvidence struct {
	// Identification
	ID            string       `json:"id"`
	DedupKey      string       `json:"dedup_key"` // Content hash for deduplication

	// Source
	SourceAgent   string       `json:"source_agent"` // Agent that collected it
	Type          EvidenceType `json:"type"`
	Service       string       `json:"service,omitempty"`

	// Content
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	Data          interface{} `json:"data,omitempty"`

	// Time window when evidence was observed
	StartTime     time.Time   `json:"start_time"`
	EndTime       time.Time   `json:"end_time"`

	// Relevance
	Confidence    float64     `json:"confidence"`    // 0-1, how relevant to incident
	Severity      string      `json:"severity,omitempty"`

	// Correlation
	IncidentID    string      `json:"incident_id,omitempty"`
	CorrelatedTo  []string    `json:"correlated_to,omitempty"` // Other evidence IDs

	// Metadata
	Tags          []string    `json:"tags,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`

	// Timing
	CollectedAt   time.Time   `json:"collected_at"`
	SharedAt      time.Time   `json:"shared_at,omitempty"`

	// Endorsements: agents that have corroborated this evidence
	EndorsedBy    []string    `json:"endorsed_by,omitempty"`
	ContradictedBy []string   `json:"contradicted_by,omitempty"`
}

// ComputeDedupKey computes a content-based dedup key for an evidence.
// Two pieces of evidence with the same type, title, service, and data hash
// are considered duplicates regardless of which agent collected them.
func (e *SharedEvidence) ComputeDedupKey() string {
	if e.DedupKey != "" {
		return e.DedupKey
	}

	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%v|%s|%s",
		e.Type, e.Title, e.Service, e.Data, e.StartTime.Format(time.RFC3339), e.EndTime.Format(time.RFC3339))
	e.DedupKey = hex.EncodeToString(h.Sum(nil))[:16]
	return e.DedupKey
}

// MergeStrategy defines how conflicting evidence is merged.
type MergeStrategy int

const (
	// MergeStrategyUnion keeps all evidence (no dedup by content, only by key)
	MergeStrategyUnion MergeStrategy = iota
	// MergeStrategyLatest keeps the most recently collected evidence per dedup key
	MergeStrategyLatest
	// MergeStrategyHighestConfidence keeps the highest-confidence evidence per dedup key
	MergeStrategyHighestConfidence
	// MergeStrategyEndorsed prefers evidence endorsed by more agents
	MergeStrategyEndorsed
)

// EvidenceStore stores shared evidence with deduplication.
type EvidenceStore struct {
	mu              sync.RWMutex
	evidence        map[string]*SharedEvidence // dedupKey -> evidence
	mergeStrategy   MergeStrategy
	conflictTracker *ConflictTracker
}

// NewEvidenceStore creates a new evidence store.
func NewEvidenceStore(strategy MergeStrategy) *EvidenceStore {
	return &EvidenceStore{
		evidence:        make(map[string]*SharedEvidence),
		mergeStrategy:   strategy,
		conflictTracker: NewConflictTracker(),
	}
}

// Add adds or merges evidence into the store. Returns the stored evidence and
// whether it was newly added (true) or merged/deduped (false).
func (s *EvidenceStore) Add(evidence *SharedEvidence) (*SharedEvidence, bool) {
	if evidence == nil {
		return nil, false
	}

	// Ensure dedup key is set
	evidence.ComputeDedupKey()

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, found := s.evidence[evidence.DedupKey]
	if !found {
		// New evidence
		if evidence.CollectedAt.IsZero() {
			evidence.CollectedAt = time.Now()
		}
		s.evidence[evidence.DedupKey] = evidence
		return evidence, true
	}

	// Merge with existing based on strategy
	merged := s.mergeEvidence(existing, evidence)
	s.evidence[evidence.DedupKey] = merged
	return merged, false
}

// mergeEvidence merges two pieces of evidence based on the configured strategy.
func (s *EvidenceStore) mergeEvidence(existing, incoming *SharedEvidence) *SharedEvidence {
	// Always union endorsements and tags (these are additive)
	merged := *existing // shallow copy

	// Union endorsements
	endorsements := make(map[string]bool)
	for _, e := range existing.EndorsedBy {
		endorsements[e] = true
	}
	if incoming.SourceAgent != "" {
		endorsements[incoming.SourceAgent] = true
	}
	for _, e := range incoming.EndorsedBy {
		endorsements[e] = true
	}
	merged.EndorsedBy = keysSorted(endorsements)

	// Union tags
	tags := make(map[string]bool)
	for _, t := range existing.Tags {
		tags[t] = true
	}
	for _, t := range incoming.Tags {
		tags[t] = true
	}
	merged.Tags = keysSorted(tags)

	// Union annotations
	if merged.Annotations == nil {
		merged.Annotations = make(map[string]string)
	}
	for k, v := range incoming.Annotations {
		if _, ok := merged.Annotations[k]; !ok {
			merged.Annotations[k] = v
		}
	}

	// Union correlated-to
	correlated := make(map[string]bool)
	for _, c := range existing.CorrelatedTo {
		correlated[c] = true
	}
	for _, c := range incoming.CorrelatedTo {
		correlated[c] = true
	}
	merged.CorrelatedTo = keysSorted(correlated)

	// Apply strategy for the "winner" fields
	switch s.mergeStrategy {
	case MergeStrategyLatest:
		if !incoming.CollectedAt.IsZero() && incoming.CollectedAt.After(existing.CollectedAt) {
			merged.Data = incoming.Data
			merged.Description = incoming.Description
			merged.Confidence = incoming.Confidence
			merged.CollectedAt = incoming.CollectedAt
		}
	case MergeStrategyHighestConfidence:
		if incoming.Confidence > existing.Confidence {
			merged.Data = incoming.Data
			merged.Description = incoming.Description
			merged.Confidence = incoming.Confidence
		}
	case MergeStrategyEndorsed:
		if len(incoming.EndorsedBy) > len(existing.EndorsedBy) {
			merged.Data = incoming.Data
			merged.Description = incoming.Description
			merged.Confidence = incoming.Confidence
		}
	case MergeStrategyUnion:
		// Keep existing data; just union the metadata above
		// Take the higher confidence of the two
		if incoming.Confidence > merged.Confidence {
			merged.Confidence = incoming.Confidence
		}
	}

	return &merged
}

// keysSorted returns map keys as a sorted slice.
func keysSorted(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Get retrieves evidence by dedup key.
func (s *EvidenceStore) Get(dedupKey string) (*SharedEvidence, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.evidence[dedupKey]
	return e, ok
}

// GetAll returns all stored evidence.
func (s *EvidenceStore) GetAll() []*SharedEvidence {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*SharedEvidence, 0, len(s.evidence))
	for _, e := range s.evidence {
		// Return copies to prevent external mutation
		copy := *e
		result = append(result, &copy)
	}
	return result
}

// GetByType returns evidence of a specific type.
func (s *EvidenceStore) GetByType(evType EvidenceType) []*SharedEvidence {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*SharedEvidence
	for _, e := range s.evidence {
		if e.Type == evType {
			copy := *e
			result = append(result, &copy)
		}
	}
	return result
}

// GetByIncident returns evidence for a specific incident.
func (s *EvidenceStore) GetByIncident(incidentID string) []*SharedEvidence {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*SharedEvidence
	for _, e := range s.evidence {
		if e.IncidentID == incidentID {
			copy := *e
			result = append(result, &copy)
		}
	}
	return result
}

// Endorse marks that an agent corroborates a piece of evidence.
func (s *EvidenceStore) Endorse(dedupKey, agentID string, agrees bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.evidence[dedupKey]
	if !ok {
		return fmt.Errorf("evidence not found: %s", dedupKey)
	}

	if agrees {
		// Add to endorsers if not already present
		for _, a := range e.EndorsedBy {
			if a == agentID {
				return nil // Already endorsed
			}
		}
		e.EndorsedBy = append(e.EndorsedBy, agentID)
	} else {
		// Add to contradictors
		for _, a := range e.ContradictedBy {
			if a == agentID {
				return nil
			}
		}
		e.ContradictedBy = append(e.ContradictedBy, agentID)

		// Track the disagreement as a potential conflict
		s.conflictTracker.RecordDisagreement(dedupKey, e.EndorsedBy, e.ContradictedBy)
	}

	return nil
}

// Remove removes evidence by dedup key.
func (s *EvidenceStore) Remove(dedupKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.evidence[dedupKey]; ok {
		delete(s.evidence, dedupKey)
		return true
	}
	return false
}

// Count returns the number of stored evidence items.
func (s *EvidenceStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.evidence)
}

// EvidenceSharer coordinates evidence sharing between agents via the broker.
type EvidenceSharer struct {
	broker *Broker
	store  *EvidenceStore
	agentID string
}

// NewEvidenceSharer creates a new evidence sharer for an agent.
func NewEvidenceSharer(broker *Broker, store *EvidenceStore, agentID string) *EvidenceSharer {
	return &EvidenceSharer{
		broker:  broker,
		store:   store,
		agentID: agentID,
	}
}

// Share broadcasts evidence to all other agents.
func (s *EvidenceSharer) Share(ctx context.Context, evidence *SharedEvidence) error {
	if evidence == nil {
		return fmt.Errorf("evidence is nil")
	}

	// Add to local store first
	evidence.SourceAgent = s.agentID
	if evidence.SharedAt.IsZero() {
		evidence.SharedAt = time.Now()
	}
	s.store.Add(evidence)

	// Broadcast to other agents
	msg := NewEvidenceShareMessage(s.agentID, evidence)
	return s.broker.Send(ctx, msg)
}

// ShareBatch broadcasts multiple evidence items.
func (s *EvidenceSharer) ShareBatch(ctx context.Context, evidence []*SharedEvidence) error {
	for _, e := range evidence {
		if err := s.Share(ctx, e); err != nil {
			return fmt.Errorf("failed to share evidence %s: %w", e.ID, err)
		}
	}
	return nil
}

// ReceiveEvidence handles an incoming evidence-share message.
func (s *EvidenceSharer) ReceiveEvidence(msg *Message) (*SharedEvidence, bool, error) {
	if msg.Type != MessageTypeEvidenceShare {
		return nil, false, fmt.Errorf("not an evidence share message")
	}

	evidence, ok := msg.Payload.(*SharedEvidence)
	if !ok {
		return nil, false, fmt.Errorf("invalid evidence payload")
	}

	// Add to local store (handles dedup/merge)
	stored, added := s.store.Add(evidence)
	return stored, added, nil
}

// RequestEvidence queries another agent for evidence on a topic.
func (s *EvidenceSharer) RequestEvidence(ctx context.Context, targetAgent, incidentID string, timeout time.Duration) ([]*SharedEvidence, error) {
	req := NewQueryMessage(s.agentID, targetAgent, "evidence request", map[string]string{
		"incident_id": incidentID,
	})

	resp, err := s.broker.SendSync(ctx, req, timeout)
	if err != nil {
		return nil, fmt.Errorf("evidence request failed: %w", err)
	}

	evidence, ok := resp.Payload.([]*SharedEvidence)
	if !ok {
		return nil, fmt.Errorf("invalid evidence response payload")
	}

	return evidence, nil
}

// HandleEvidenceRequest responds to an evidence request.
func (s *EvidenceSharer) HandleEvidenceRequest(req *Message) (*Message, error) {
	params, ok := req.Payload.(map[string]string)
	if !ok {
		return nil, fmt.Errorf("invalid request params")
	}

	incidentID := params["incident_id"]
	var evidence []*SharedEvidence
	if incidentID != "" {
		evidence = s.store.GetByIncident(incidentID)
	} else {
		evidence = s.store.GetAll()
	}

	resp := NewResponseMessage(s.agentID, req.From, req.ID, req.ID, evidence)
	return resp, nil
}

// GetConsensusEvidence returns evidence endorsed by multiple agents.
func (s *EvidenceSharer) GetConsensusEvidence(minEndorsers int) []*SharedEvidence {
	all := s.store.GetAll()

	var consensus []*SharedEvidence
	for _, e := range all {
		if len(e.EndorsedBy) >= minEndorsers {
			consensus = append(consensus, e)
		}
	}

	// Sort by endorsement count descending
	sort.Slice(consensus, func(i, j int) bool {
		return len(consensus[i].EndorsedBy) > len(consensus[j].EndorsedBy)
	})

	return consensus
}

// ConflictTracker tracks disagreements between agents about evidence.
type ConflictTracker struct {
	mu            sync.RWMutex
	conflicts     map[string]*EvidenceConflict // dedupKey -> conflict
}

// EvidenceConflict represents a disagreement about a piece of evidence.
type EvidenceConflict struct {
	EvidenceKey    string    `json:"evidence_key"`
	Supporters     []string  `json:"supporters"`     // Agents that endorse
	Dissenters     []string  `json:"dissenters"`     // Agents that contradict
	DetectedAt     time.Time `json:"detected_at"`
	ResolvedAt     time.Time `json:"resolved_at,omitempty"`
	Resolution     string    `json:"resolution,omitempty"`
}

// NewConflictTracker creates a new conflict tracker.
func NewConflictTracker() *ConflictTracker {
	return &ConflictTracker{
		conflicts: make(map[string]*EvidenceConflict),
	}
}

// RecordDisagreement records a disagreement about evidence.
func (c *ConflictTracker) RecordDisagreement(evidenceKey string, supporters, dissenters []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	conflict, ok := c.conflicts[evidenceKey]
	if !ok {
		conflict = &EvidenceConflict{
			EvidenceKey: evidenceKey,
			DetectedAt:  time.Now(),
		}
		c.conflicts[evidenceKey] = conflict
	}

	conflict.Supporters = uniqueAppend(conflict.Supporters, supporters)
	conflict.Dissenters = uniqueAppend(conflict.Dissenters, dissenters)
}

// GetConflicts returns all unresolved conflicts.
func (c *ConflictTracker) GetConflicts() []*EvidenceConflict {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*EvidenceConflict
	for _, conflict := range c.conflicts {
		if conflict.ResolvedAt.IsZero() {
			copy := *conflict
			result = append(result, &copy)
		}
	}
	return result
}

// ResolveConflict marks a conflict as resolved.
func (c *ConflictTracker) ResolveConflict(evidenceKey, resolution string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	conflict, ok := c.conflicts[evidenceKey]
	if !ok {
		return fmt.Errorf("conflict not found: %s", evidenceKey)
	}

	conflict.ResolvedAt = time.Now()
	conflict.Resolution = resolution
	return nil
}

// uniqueAppend appends items to a slice, skipping duplicates.
func uniqueAppend(slice []string, items []string) []string {
	seen := make(map[string]bool)
	for _, s := range slice {
		seen[s] = true
	}
	for _, item := range items {
		if !seen[item] {
			slice = append(slice, item)
			seen[item] = true
		}
	}
	return slice
}

// EvidenceSummary summarizes the evidence store for an incident.
type EvidenceSummary struct {
	IncidentID      string                  `json:"incident_id"`
	TotalEvidence   int                     `json:"total_evidence"`
	ByType          map[EvidenceType]int    `json:"by_type"`
	ConsensusCount  int                     `json:"consensus_count"`    // Endorsed by >=2
	ConflictedCount int                     `json:"conflicted_count"`
	TopEvidence     []*SharedEvidence       `json:"top_evidence"`       // Highest confidence
	Conflicts       []*EvidenceConflict     `json:"conflicts,omitempty"`
}

// Summarize generates a summary of evidence for an incident.
func (s *EvidenceStore) Summarize(incidentID string) *EvidenceSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	summary := &EvidenceSummary{
		IncidentID: incidentID,
		ByType:     make(map[EvidenceType]int),
	}

	var all []*SharedEvidence
	for _, e := range s.evidence {
		if incidentID == "" || e.IncidentID == incidentID {
			all = append(all, e)
			summary.ByType[e.Type]++
			if len(e.EndorsedBy) >= 2 {
				summary.ConsensusCount++
			}
			if len(e.ContradictedBy) > 0 {
				summary.ConflictedCount++
			}
		}
	}

	summary.TotalEvidence = len(all)

	// Sort by confidence descending for top evidence
	sort.Slice(all, func(i, j int) bool {
		return all[i].Confidence > all[j].Confidence
	})

	limit := 5
	if len(all) < limit {
		limit = len(all)
	}
	summary.TopEvidence = all[:limit]

	// Include conflicts
	summary.Conflicts = s.conflictTracker.GetConflicts()

	return summary
}
