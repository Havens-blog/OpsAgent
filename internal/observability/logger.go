// Package observability provides structured logging functionality.
package observability

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LogLevel represents the severity level of a log entry.
type LogLevel string

const (
	// LevelDebug represents debug level logs
	LevelDebug LogLevel = "debug"
	// LevelInfo represents info level logs
	LevelInfo LogLevel = "info"
	// LevelWarn represents warning level logs
	LevelWarn LogLevel = "warn"
	// LevelError represents error level logs
	LevelError LogLevel = "error"
	// LevelFatal represents fatal level logs
	LevelFatal LogLevel = "fatal"
)

// LogFormat represents the format of log output.
type LogFormat string

const (
	// FormatJSON represents JSON log format
	FormatJSON LogFormat = "json"
	// FormatText represents text log format
	FormatText LogFormat = "text"
)

// Logger provides structured logging capabilities.
type Logger struct {
	mu           sync.RWMutex
	zapLogger    *zap.Logger
	sugarLogger  *zap.SugaredLogger
	level        zapcore.Level
	format       LogFormat
	serviceName  string
	serviceVersion string
}

// NewLogger creates a new logger with the specified configuration.
func NewLogger(serviceName, level string, format LogFormat) (*Logger, error) {
	// Parse log level
	zapLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		zapLevel = zapcore.InfoLevel
	}

	// Create encoder config
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Create encoder
	var encoder zapcore.Encoder
	if format == FormatJSON {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	// Create core
	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(os.Stdout),
		zapLevel,
	)

	// Create logger
	zapLogger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return &Logger{
		zapLogger:    zapLogger,
		sugarLogger:  zapLogger.Sugar(),
		level:        zapLevel,
		format:       format,
		serviceName:  serviceName,
		serviceVersion: "dev",
	}, nil
}

// NewLoggerWithOutput creates a new logger with custom output.
func NewLoggerWithOutput(serviceName, level string, format LogFormat, output io.Writer) (*Logger, error) {
	logger, err := NewLogger(serviceName, level, format)
	if err != nil {
		return nil, err
	}

	logger.mu.Lock()
	defer logger.mu.Unlock()

	// Create new core with custom output
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "message",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	var encoder zapcore.Encoder
	if format == FormatJSON {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(output),
		logger.level,
	)

	logger.zapLogger = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	logger.sugarLogger = logger.zapLogger.Sugar()

	return logger, nil
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, fields ...Field) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	zapFields := l.convertFields(fields)
	l.sugarLogger.Debugw(msg, zapFields...)
}

// Info logs an info message.
func (l *Logger) Info(msg string, fields ...Field) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	zapFields := l.convertFields(fields)
	l.sugarLogger.Infow(msg, zapFields...)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, fields ...Field) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	zapFields := l.convertFields(fields)
	l.sugarLogger.Warnw(msg, zapFields...)
}

// Error logs an error message.
func (l *Logger) Error(msg string, fields ...Field) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	zapFields := l.convertFields(fields)
	l.sugarLogger.Errorw(msg, zapFields...)
}

// Fatal logs a fatal message and exits.
func (l *Logger) Fatal(msg string, fields ...Field) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	zapFields := l.convertFields(fields)
	l.sugarLogger.Fatalw(msg, zapFields...)
}

// With creates a child logger with additional fields.
func (l *Logger) With(fields ...Field) *Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	zapFields := l.convertFields(fields)
	return &Logger{
		zapLogger:    l.zapLogger.With(zapFields...),
		sugarLogger:  l.sugarLogger.With(zapFields...),
		level:        l.level,
		format:       l.format,
		serviceName:  l.serviceName,
		serviceVersion: l.serviceVersion,
	}
}

// WithServiceName adds a service name to the logger.
func (l *Logger) WithServiceName(name string) *Logger {
	return l.With(String("service", name))
}

// WithSessionID adds a session ID to the logger.
func (l *Logger) WithSessionID(sessionID string) *Logger {
	return l.With(String("session_id", sessionID))
}

// WithAgentName adds an agent name to the logger.
func (l *Logger) WithAgentName(agentName string) *Logger {
	return l.With(String("agent", agentName))
}

// WithTraceID adds a trace ID to the logger.
func (l *Logger) WithTraceID(traceID string) *Logger {
	return l.With(String("trace_id", traceID))
}

// convertFields converts custom fields to zap fields.
func (l *Logger) convertFields(fields []Field) []interface{} {
	result := make([]interface{}, 0, len(fields)*2)
	for _, f := range fields {
		result = append(result, f.key, f.value)
	}
	return result
}

// Sync flushes any buffered log entries.
func (l *Logger) Sync() error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.zapLogger.Sync()
}

// Close closes the logger.
func (l *Logger) Close() error {
	return l.Sync()
}

// Field represents a log field.
type Field struct {
	key   string
	value interface{}
}

// String creates a string field.
func String(key, value string) Field {
	return Field{key: key, value: value}
}

// Int creates an integer field.
func Int(key string, value int) Field {
	return Field{key: key, value: value}
}

// Int64 creates an int64 field.
func Int64(key string, value int64) Field {
	return Field{key: key, value: value}
}

// Float64 creates a float64 field.
func Float64(key string, value float64) Field {
	return Field{key: key, value: value}
}

// Bool creates a boolean field.
func Bool(key string, value bool) Field {
	return Field{key: key, value: value}
}

// Duration creates a duration field.
func Duration(key string, value time.Duration) Field {
	return Field{key: key, value: value}
}

// Err creates an error field.
func Err(err error) Field {
	return Field{key: "error", value: err.Error()}
}

// Any creates a field with any type.
func Any(key string, value interface{}) Field {
	return Field{key: key, value: value}
}

// Context provides context-aware logging helpers.
type Context struct {
	logger    *Logger
	sessionID string
	agentName string
	traceID   string
}

// NewContext creates a new logging context.
func NewContext(logger *Logger) *Context {
	return &Context{
		logger: logger,
	}
}

// WithSessionID sets the session ID.
func (c *Context) WithSessionID(sessionID string) *Context {
	c.sessionID = sessionID
	return c
}

// WithAgentName sets the agent name.
func (c *Context) WithAgentName(agentName string) *Context {
	c.agentName = agentName
	return c
}

// WithTraceID sets the trace ID.
func (c *Context) WithTraceID(traceID string) *Context {
	c.traceID = traceID
	return c
}

// Debug logs a debug message with context.
func (c *Context) Debug(msg string, fields ...Field) {
	logger := c.logger
	if c.sessionID != "" {
		logger = logger.WithSessionID(c.sessionID)
	}
	if c.agentName != "" {
		logger = logger.WithAgentName(c.agentName)
	}
	if c.traceID != "" {
		logger = logger.WithTraceID(c.traceID)
	}
	logger.Debug(msg, fields...)
}

// Info logs an info message with context.
func (c *Context) Info(msg string, fields ...Field) {
	logger := c.logger
	if c.sessionID != "" {
		logger = logger.WithSessionID(c.sessionID)
	}
	if c.agentName != "" {
		logger = logger.WithAgentName(c.agentName)
	}
	if c.traceID != "" {
		logger = logger.WithTraceID(c.traceID)
	}
	logger.Info(msg, fields...)
}

// Warn logs a warning message with context.
func (c *Context) Warn(msg string, fields ...Field) {
	logger := c.logger
	if c.sessionID != "" {
		logger = logger.WithSessionID(c.sessionID)
	}
	if c.agentName != "" {
		logger = logger.WithAgentName(c.agentName)
	}
	if c.traceID != "" {
		logger = logger.WithTraceID(c.traceID)
	}
	logger.Warn(msg, fields...)
}

// Error logs an error message with context.
func (c *Context) Error(msg string, fields ...Field) {
	logger := c.logger
	if c.sessionID != "" {
		logger = logger.WithSessionID(c.sessionID)
	}
	if c.agentName != "" {
		logger = logger.WithAgentName(c.agentName)
	}
	if c.traceID != "" {
		logger = logger.WithTraceID(c.traceID)
	}
	logger.Error(msg, fields...)
}

// Global logger instance
var (
	globalLogger *Logger
	once         sync.Once
)

// InitGlobalLogger initializes the global logger.
func InitGlobalLogger(serviceName, level string, format LogFormat) error {
	var err error
	once.Do(func() {
		globalLogger, err = NewLogger(serviceName, level, format)
	})
	return err
}

// GetGlobalLogger returns the global logger instance.
func GetGlobalLogger() *Logger {
	if globalLogger == nil {
		// Initialize with defaults
		globalLogger, _ = NewLogger("opsagent", "info", FormatJSON)
	}
	return globalLogger
}

// L is a convenience function to get the global logger.
func L() *Logger {
	return GetGlobalLogger()
}

// WithContext creates a new logging context.
func WithContext() *Context {
	return NewContext(GetGlobalLogger())
}
