// Package observability provides audit logging functionality.
package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEventType represents the type of audit event.
type AuditEventType string

const (
	// EventAgentStart represents an agent start event
	EventAgentStart AuditEventType = "agent_start"
	// EventAgentStop represents an agent stop event
	EventAgentStop AuditEventType = "agent_stop"
	// EventToolCall represents a tool call event
	EventToolCall AuditEventType = "tool_call"
	// EventToolResult represents a tool result event
	EventToolResult AuditEventType = "tool_result"
	// EventApprovalRequest represents an approval request event
	EventApprovalRequest AuditEventType = "approval_request"
	// EventApprovalGranted represents an approval granted event
	EventApprovalGranted AuditEventType = "approval_granted"
	// EventApprovalDenied represents an approval denied event
	EventApprovalDenied AuditEventType = "approval_denied"
	// EventDataSourceQuery represents a data source query event
	EventDataSourceQuery AuditEventType = "datasource_query"
	// EventConfigChange represents a configuration change event
	EventConfigChange AuditEventType = "config_change"
)

// AuditEvent represents an audit log entry.
type AuditEvent struct {
	// Timestamp is when the event occurred
	Timestamp time.Time `json:"timestamp"`
	// EventType is the type of the event
	EventType AuditEventType `json:"event_type"`
	// SessionID is the unique session identifier
	SessionID string `json:"session_id,omitempty"`
	// AgentName is the name of the agent
	AgentName string `json:"agent_name,omitempty"`
	// ToolName is the name of the tool
	ToolName string `json:"tool_name,omitempty"`
	// Action is the action performed
	Action string `json:"action,omitempty"`
	// Parameters are the parameters passed (sensitive data should be redacted)
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	// ApprovalID is the identifier for the approval
	ApprovalID string `json:"approval_id,omitempty"`
	// Result is the result of the operation
	Result string `json:"result,omitempty"`
	// Error is the error message if the operation failed
	Error string `json:"error,omitempty"`
	// Duration is how long the operation took
	Duration time.Duration `json:"duration_ms,omitempty"`
	// Metadata contains additional event-specific data
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// AuditWriter defines the interface for writing audit events.
type AuditWriter interface {
	Write(event *AuditEvent) error
	Close() error
}

// FileAuditWriter writes audit events to a file.
type FileAuditWriter struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	encoder  *json.Encoder
}

// NewFileAuditWriter creates a new file-based audit writer.
func NewFileAuditWriter(path string) (*FileAuditWriter, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audit directory: %w", err)
	}

	// Open file in append mode
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit file: %w", err)
	}

	return &FileAuditWriter{
		file:    file,
		path:    path,
		encoder: json.NewEncoder(file),
	}, nil
}

// Write writes an audit event to the file.
func (w *FileAuditWriter) Write(event *AuditEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Ensure timestamp is set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Redact sensitive parameters
	event = w.redactSensitiveData(event)

	// Write to file
	if err := w.encoder.Encode(event); err != nil {
		return fmt.Errorf("failed to write audit event: %w", err)
	}

	return nil
}

// Close closes the audit file.
func (w *FileAuditWriter) Close() error {
	return w.file.Close()
}

// redactSensitiveData removes sensitive data from the audit event.
func (w *FileAuditWriter) redactSensitiveData(event *AuditEvent) *AuditEvent {
	// Create a copy to avoid mutating the original
	redacted := *event
	if event.Parameters != nil {
		redacted.Parameters = make(map[string]interface{})
		for k, v := range event.Parameters {
			redacted.Parameters[k] = w.redactValue(k, v)
		}
	}
	if event.Metadata != nil {
		redacted.Metadata = make(map[string]interface{})
		for k, v := range event.Metadata {
			redacted.Metadata[k] = w.redactValue(k, v)
		}
	}
	return &redacted
}

// redactValue redacts sensitive values based on key names.
func (w *FileAuditWriter) redactValue(key string, value interface{}) interface{} {
	sensitiveKeys := []string{
		"password", "passwd", "pwd", "secret", "token", "key",
		"api_key", "apikey", "api-key", "access_key", "access_key",
		"secret_key", "secretkey", "secret-key", "private_key",
		"auth", "authorization", "credential", "credentials",
	}

	lowerKey := strings.ToLower(key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(lowerKey, sensitive) {
			return "***REDACTED***"
		}
	}

	// Recursively handle maps
	if m, ok := value.(map[string]interface{}); ok {
		redacted := make(map[string]interface{})
		for k, v := range m {
			redacted[k] = w.redactValue(k, v)
		}
		return redacted
	}

	return value
}

// strings package is needed for strings.ToLower and strings.Contains
import "strings"

// AuditLogger manages audit logging.
type AuditLogger struct {
	writer AuditWriter
	enabled bool
}

// NewAuditLogger creates a new audit logger.
func NewAuditLogger(writer AuditWriter, enabled bool) *AuditLogger {
	return &AuditLogger{
		writer:  writer,
		enabled: enabled,
	}
}

// LogEvent logs an audit event.
func (l *AuditLogger) LogEvent(event *AuditEvent) error {
	if !l.enabled {
		return nil
	}
	if l.writer == nil {
		return fmt.Errorf("audit writer not configured")
	}
	return l.writer.Write(event)
}

// LogAgentStart logs an agent start event.
func (l *AuditLogger) LogAgentStart(ctx context.Context, sessionID, agentName string, params map[string]interface{}) error {
	return l.LogEvent(&AuditEvent{
		EventType:  EventAgentStart,
		Timestamp:  time.Now(),
		SessionID:  sessionID,
		AgentName:  agentName,
		Parameters: params,
		Result:     "started",
	})
}

// LogAgentStop logs an agent stop event.
func (l *AuditLogger) LogAgentStop(ctx context.Context, sessionID, agentName string, duration time.Duration, err error) error {
	event := &AuditEvent{
		EventType: EventAgentStop,
		Timestamp: time.Now(),
		SessionID: sessionID,
		AgentName: agentName,
		Duration:  duration,
	}

	if err != nil {
		event.Result = "failed"
		event.Error = err.Error()
	} else {
		event.Result = "completed"
	}

	return l.LogEvent(event)
}

// LogToolCall logs a tool call event.
func (l *AuditLogger) LogToolCall(ctx context.Context, sessionID, agentName, toolName string, params map[string]interface{}) error {
	return l.LogEvent(&AuditEvent{
		EventType:  EventToolCall,
		Timestamp:  time.Now(),
		SessionID:  sessionID,
		AgentName:  agentName,
		ToolName:   toolName,
		Parameters: params,
	})
}

// LogToolResult logs a tool result event.
func (l *AuditLogger) LogToolResult(ctx context.Context, sessionID, agentName, toolName string, duration time.Duration, result interface{}, err error) error {
	event := &AuditEvent{
		EventType: EventToolResult,
		Timestamp: time.Now(),
		SessionID: sessionID,
		AgentName: agentName,
		ToolName:  toolName,
		Duration:  duration,
	}

	if err != nil {
		event.Result = "failed"
		event.Error = err.Error()
	} else {
		event.Result = "success"
		// Store a summary of the result, not the full result
		event.Metadata = map[string]interface{}{
			"result_type": fmt.Sprintf("%T", result),
		}
	}

	return l.LogEvent(event)
}

// LogApprovalRequest logs an approval request event.
func (l *AuditLogger) LogApprovalRequest(ctx context.Context, sessionID, agentName, toolName, reason string, params map[string]interface{}) error {
	return l.LogEvent(&AuditEvent{
		EventType:  EventApprovalRequest,
		Timestamp:  time.Now(),
		SessionID:  sessionID,
		AgentName:  agentName,
		ToolName:   toolName,
		Action:     reason,
		Parameters: params,
		Result:     "pending",
	})
}

// LogApprovalGranted logs an approval granted event.
func (l *AuditLogger) LogApprovalGranted(ctx context.Context, sessionID, approvalID string) error {
	return l.LogEvent(&AuditEvent{
		EventType:   EventApprovalGranted,
		Timestamp:   time.Now(),
		SessionID:   sessionID,
		ApprovalID:  approvalID,
		Result:      "granted",
	})
}

// LogApprovalDenied logs an approval denied event.
func (l *AuditLogger) LogApprovalDenied(ctx context.Context, sessionID, approvalID, reason string) error {
	return l.LogEvent(&AuditEvent{
		EventType:   EventApprovalDenied,
		Timestamp:   time.Now(),
		SessionID:   sessionID,
		ApprovalID:  approvalID,
		Action:      reason,
		Result:      "denied",
	})
}

// LogDataSourceQuery logs a data source query event.
func (l *AuditLogger) LogDataSourceQuery(ctx context.Context, sessionID, agentName, source string, query map[string]interface{}, duration time.Duration, err error) error {
	event := &AuditEvent{
		EventType:  EventDataSourceQuery,
		Timestamp:  time.Now(),
		SessionID:  sessionID,
		AgentName:  agentName,
		Action:     source,
		Parameters: query,
		Duration:   duration,
	}

	if err != nil {
		event.Result = "failed"
		event.Error = err.Error()
	} else {
		event.Result = "success"
	}

	return l.LogEvent(event)
}

// Close closes the audit logger.
func (l *AuditLogger) Close() error {
	if l.writer != nil {
		return l.writer.Close()
	}
	return nil
}

// IsEnabled returns true if audit logging is enabled.
func (l *AuditLogger) IsEnabled() bool {
	return l.enabled
}
