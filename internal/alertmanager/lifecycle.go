// Package alertmanager provides incident lifecycle management.
package alertmanager

import (
	"context"
	"fmt"
	"time"
)

// LifecycleState represents the state of an incident in its lifecycle.
type LifecycleState string

const (
	// StateDetected is when an alert is first received
	StateDetected LifecycleState = "detected"
	// StateInvestigating is when RCA analysis is in progress
	StateInvestigating LifecycleState = "investigating"
	// StateIdentified is when root cause is identified
	StateIdentified LifecycleState = "identified"
	// StateResolved is when the incident is resolved
	StateResolved LifecycleState = "resolved"
	// StateClosed is when the incident is archived
	StateClosed LifecycleState = "closed"
)

// IncidentLifecycle manages the state transitions of an incident.
type IncidentLifecycle struct {
	incident    *Incident
	currentState LifecycleState
	history      []LifecycleTransition
	store        IncidentStore
}

// LifecycleTransition represents a state transition.
type LifecycleTransition struct {
	From      LifecycleState
	To        LifecycleState
	Timestamp time.Time
	Reason    string
	User      string
}

// NewIncidentLifecycle creates a new lifecycle manager for an incident.
func NewIncidentLifecycle(incident *Incident, store IncidentStore) *IncidentLifecycle {
	return &IncidentLifecycle{
		incident:    incident,
		currentState: StateDetected,
		history: []LifecycleTransition{
			{
				From:      "",
				To:        StateDetected,
				Timestamp: time.Now(),
				Reason:    "Incident created",
			},
		},
		store: store,
	}
}

// Transition moves the incident to a new state.
func (l *IncidentLifecycle) Transition(ctx context.Context, to LifecycleState, reason, user string) error {
	// Validate transition
	if !l.isValidTransition(l.currentState, to) {
		return fmt.Errorf("invalid transition from %s to %s", l.currentState, to)
	}

	// Record transition
	transition := LifecycleTransition{
		From:      l.currentState,
		To:        to,
		Timestamp: time.Now(),
		Reason:    reason,
		User:      user,
	}

	l.history = append(l.history, transition)
	l.currentState = to

	// Update incident status based on state
	l.incident.Status = mapStateToStatus(to)

	// Persist changes
	if l.store != nil {
		if err := l.store.Update(ctx, *l.incident); err != nil {
			return fmt.Errorf("failed to persist transition: %w", err)
		}
	}

	return nil
}

// CurrentState returns the current lifecycle state.
func (l *IncidentLifecycle) CurrentState() LifecycleState {
	return l.currentState
}

// History returns the transition history.
func (l *IncidentLifecycle) History() []LifecycleTransition {
	return l.history
}

// CanTransition checks if a transition is valid.
func (l *IncidentLifecycle) CanTransition(to LifecycleState) bool {
	return l.isValidTransition(l.currentState, to)
}

// isValidTransition validates state transitions.
func (l *IncidentLifecycle) isValidTransition(from, to LifecycleState) bool {
	// Define valid transitions
	validTransitions := map[LifecycleState][]LifecycleState{
		StateDetected: {
			StateInvestigating,
			StateResolved,
		},
		StateInvestigating: {
			StateIdentified,
			StateResolved,
		},
		StateIdentified: {
			StateResolved,
		},
		StateResolved: {
			StateClosed,
		},
		StateClosed: {}, // No transitions from closed
	}

	allowed, exists := validTransitions[from]
	if !exists {
		return false
	}

	for _, state := range allowed {
		if state == to {
			return true
		}
	}

	return false
}

// mapStateToStatus maps lifecycle state to incident status.
func mapStateToStatus(state LifecycleState) string {
	switch state {
	case StateDetected, StateInvestigating, StateIdentified:
		return "active"
	case StateResolved, StateClosed:
		return "resolved"
	default:
		return "unknown"
	}
}

// IncidentTimer tracks time spent in each lifecycle state.
type IncidentTimer struct {
	stateEntryTimes map[LifecycleState]time.Time
	durations      map[LifecycleState]time.Duration
}

// NewIncidentTimer creates a new timer for tracking state durations.
func NewIncidentTimer() *IncidentTimer {
	return &IncidentTimer{
		stateEntryTimes: make(map[LifecycleState]time.Time),
		durations:      make(map[LifecycleState]time.Duration),
	}
}

// EnterState records entering a state.
func (t *IncidentTimer) EnterState(state LifecycleState) {
	// Record exit from previous state
	if previousState, entryTime := t.getCurrentState(); entryTime != (time.Time{}) {
		duration := time.Since(entryTime)
		t.durations[previousState] = duration
	}

	// Record entry to new state
	t.stateEntryTimes[state] = time.Now()
}

// getCurrentState returns the current state and its entry time.
func (t *IncidentTimer) getCurrentState() (LifecycleState, time.Time) {
	// Find the most recent state entry
	for _, state := range []LifecycleState{
		StateClosed, StateResolved, StateIdentified,
		StateInvestigating, StateDetected,
	} {
		if entryTime, exists := t.stateEntryTimes[state]; exists {
			return state, entryTime
		}
	}
	return "", time.Time{}
}

// GetDuration returns the duration spent in a state.
func (t *IncidentTimer) GetDuration(state LifecycleState) time.Duration {
	// If state is current, calculate from entry time
	if entryTime, isCurrent := t.stateEntryTimes[state]; isCurrent {
		return time.Since(entryTime)
	}

	// Otherwise return recorded duration
	return t.durations[state]
}

// GetTotalDuration returns the total duration of the incident.
func (t *IncidentTimer) GetTotalDuration() time.Duration {
	var total time.Duration
	for _, duration := range t.durations {
		total += duration
	}

	// Add current state duration
	if _, entryTime := t.getCurrentState(); entryTime != (time.Time{}) {
		total += time.Since(entryTime)
	}

	return total
}
