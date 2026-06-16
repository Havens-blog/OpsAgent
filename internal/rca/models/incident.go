// Package models defines the data structures used throughout the RCA system.
package models

import (
	"fmt"
	"time"
)

// Incident represents a production incident that requires root cause analysis.
type Incident struct {
	// Unique identifier for this incident
	ID string

	// When the incident was detected
	DetectedAt time.Time

	// The time window when the incident occurred
	StartTime time.Time
	EndTime   time.Time

	// Severity level (P0, P1, P2, P3)
	Severity string

	// Services affected by this incident
	AffectedServices []string

	// Initial description or alert message
	Description string

	// Source of the incident (alert name, manual report, etc.)
	Source string

	// Additional labels and metadata
	Labels map[string]string
}

// Validate checks if the incident has valid fields.
func (i *Incident) Validate() error {
	if i.ID == "" {
		return fmt.Errorf("incident ID is required")
	}

	if i.StartTime.IsZero() {
		return fmt.Errorf("start time is required")
	}

	if i.EndTime.IsZero() {
		return fmt.Errorf("end time is required")
	}

	if i.EndTime.Before(i.StartTime) {
		return fmt.Errorf("end time must be after start time")
	}

	validSeverities := map[string]bool{
		"P0": true,
		"P1": true,
		"P2": true,
		"P3": true,
		"P4": true,
	}
	if i.Severity != "" && !validSeverities[i.Severity] {
		return fmt.Errorf("invalid severity: %s (must be P0-P4)", i.Severity)
	}

	return nil
}

// TimeWindow represents a time range for analysis.
type TimeWindow struct {
	Start time.Time
	End   time.Time
}

// Signals contains all collected data from various sources.
type Signals struct {
	Logs     []LogEntry
	Metrics  []MetricSeries
	Traces   []TraceSummary
	Topology *Topology
	Changes  []Change
}

// NormalizedSignals contains time-aligned signals for correlation.
type NormalizedSignals struct {
	TimeWindows    []TimeWindow
	LogByWindow    map[int][]LogEntry
	MetricTrends   map[string]map[int]Trend
	TraceByWindow  map[int][]TraceSummary
	Changes        []Change
	Topology       *Topology
}

// LogEntry represents a single log record.
type LogEntry struct {
	Timestamp   time.Time
	Service     string
	Level       string
	Message     string
	TraceID     string
	SpanID      string
	Labels      map[string]string
	IsError     bool
}

// MetricSeries represents a time series of metric values.
type MetricSeries struct {
	Name       string
	Labels     map[string]string
	Unit       string
	DataPoints []DataPoint
}

// DataPoint represents a single metric value at a timestamp.
type DataPoint struct {
	Timestamp time.Time
	Value     float64
}

// TraceSummary represents a distributed trace summary.
type TraceSummary struct {
	TraceID      string
	StartTime    time.Time
	Duration     time.Duration
	ServiceName  string
	OperationName string
	HasError     bool
	SpanCount    int
	Spans        []Span
}

// Span represents a single span in a trace.
type Span struct {
	SpanID        string
	ParentSpanID  string
	ServiceName   string
	OperationName string
	StartTime     time.Time
	Duration      time.Duration
	HasError      bool
	Tags          map[string]string
}

// Trend represents the direction of metric change.
type Trend int

const (
	TrendStable Trend = iota
	TrendUp
	TrendDown
)

// String returns the string representation of the trend.
func (t Trend) String() string {
	switch t {
	case TrendUp:
		return "up"
	case TrendDown:
		return "down"
	default:
		return "stable"
	}
}

// Change represents a configuration or deployment change.
type Change struct {
	ID          string
	Timestamp   time.Time
	Service     string
	Type        string // "deployment", "config", "scale"
	Description string
	Details     map[string]string
}

// Topology represents the service dependency graph.
type Topology struct {
	Nodes []ServiceNode
	Edges []DependencyEdge
}

// ServiceNode represents a service in the topology.
type ServiceNode struct {
	Name   string
	Type   string // "application", "database", "queue", "cache"
	Labels map[string]string
}

// DependencyEdge represents a dependency relationship between services.
type DependencyEdge struct {
	From string
	To   string
	Type string // "http", "rpc", "database", "queue"
}

// HasService checks if a service exists in the topology.
func (t *Topology) HasService(name string) bool {
	if t == nil {
		return false
	}
	for _, node := range t.Nodes {
		if node.Name == name {
			return true
		}
	}
	return false
}

// GetDependencies returns the dependencies of a given service.
func (t *Topology) GetDependencies(service string) []string {
	if t == nil {
		return nil
	}
	var deps []string
	for _, edge := range t.Edges {
		if edge.From == service {
			deps = append(deps, edge.To)
		}
	}
	return deps
}

// GetDependents returns services that depend on the given service.
func (t *Topology) GetDependents(service string) []string {
	if t == nil {
		return nil
	}
	var dependents []string
	for _, edge := range t.Edges {
		if edge.To == service {
			dependents = append(dependents, edge.From)
		}
	}
	return dependents
}
