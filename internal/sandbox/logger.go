package sandbox

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// LogLevel represents the severity of a log message.
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelNone // Disables all logging
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLogLevel parses a string log level.
func ParseLogLevel(s string) LogLevel {
	switch strings.ToLower(s) {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn", "warning":
		return LogLevelWarn
	case "error":
		return LogLevelError
	case "none", "off":
		return LogLevelNone
	default:
		return LogLevelInfo
	}
}

// Logger provides structured logging with levels.
type Logger struct {
	mu       sync.Mutex
	level    LogLevel
	output   io.Writer
	prefix   string
	fields   map[string]interface{}
	colorize bool
}

// LoggerOption configures a Logger.
type LoggerOption func(*Logger)

// WithLevel sets the log level.
func WithLevel(level LogLevel) LoggerOption {
	return func(l *Logger) {
		l.level = level
	}
}

// WithOutput sets the output writer.
func WithOutput(w io.Writer) LoggerOption {
	return func(l *Logger) {
		l.output = w
	}
}

// WithPrefix sets a prefix for all log messages.
func WithPrefix(prefix string) LoggerOption {
	return func(l *Logger) {
		l.prefix = prefix
	}
}

// WithColor enables/disables colorized output.
func WithColor(enabled bool) LoggerOption {
	return func(l *Logger) {
		l.colorize = enabled
	}
}

// NewLogger creates a new Logger with the given options.
func NewLogger(opts ...LoggerOption) *Logger {
	l := &Logger{
		level:    LogLevelInfo,
		output:   os.Stderr,
		fields:   make(map[string]interface{}),
		colorize: true,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// With creates a child logger with additional fields.
func (l *Logger) With(key string, value interface{}) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()

	newFields := make(map[string]interface{}, len(l.fields)+1)
	for k, v := range l.fields {
		newFields[k] = v
	}
	newFields[key] = value

	return &Logger{
		level:    l.level,
		output:   l.output,
		prefix:   l.prefix,
		fields:   newFields,
		colorize: l.colorize,
	}
}

// WithFields creates a child logger with multiple additional fields.
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()

	newFields := make(map[string]interface{}, len(l.fields)+len(fields))
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}

	return &Logger{
		level:    l.level,
		output:   l.output,
		prefix:   l.prefix,
		fields:   newFields,
		colorize: l.colorize,
	}
}

// SetLevel changes the log level.
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, args ...interface{}) {
	l.log(LogLevelDebug, msg, args...)
}

// Info logs an info message.
func (l *Logger) Info(msg string, args ...interface{}) {
	l.log(LogLevelInfo, msg, args...)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, args ...interface{}) {
	l.log(LogLevelWarn, msg, args...)
}

// Error logs an error message.
func (l *Logger) Error(msg string, args ...interface{}) {
	l.log(LogLevelError, msg, args...)
}

// Debugf logs a formatted debug message.
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log(LogLevelDebug, fmt.Sprintf(format, args...))
}

// Infof logs a formatted info message.
func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(LogLevelInfo, fmt.Sprintf(format, args...))
}

// Warnf logs a formatted warning message.
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(LogLevelWarn, fmt.Sprintf(format, args...))
}

// Errorf logs a formatted error message.
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log(LogLevelError, fmt.Sprintf(format, args...))
}

func (l *Logger) log(level LogLevel, msg string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	var b strings.Builder

	// Timestamp
	b.WriteString(time.Now().Format("15:04:05"))
	b.WriteString(" ")

	// Level with optional color
	levelStr := level.String()
	if l.colorize {
		levelStr = l.colorizeLevel(level)
	}
	b.WriteString(fmt.Sprintf("%-5s", levelStr))
	b.WriteString(" ")

	// Prefix
	if l.prefix != "" {
		b.WriteString("[")
		b.WriteString(l.prefix)
		b.WriteString("] ")
	}

	// Message
	b.WriteString(msg)

	// Key-value pairs from args (key1, val1, key2, val2, ...)
	fields := make(map[string]interface{})
	for k, v := range l.fields {
		fields[k] = v
	}
	for i := 0; i+1 < len(args); i += 2 {
		if key, ok := args[i].(string); ok {
			fields[key] = args[i+1]
		}
	}

	// Append fields
	if len(fields) > 0 {
		b.WriteString(" ")
		first := true
		for k, v := range fields {
			if !first {
				b.WriteString(" ")
			}
			first = false
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(fmt.Sprintf("%v", v))
		}
	}

	b.WriteString("\n")
	fmt.Fprint(l.output, b.String())
}

func (l *Logger) colorizeLevel(level LogLevel) string {
	switch level {
	case LogLevelDebug:
		return "\033[36mDEBUG\033[0m" // Cyan
	case LogLevelInfo:
		return "\033[32mINFO\033[0m" // Green
	case LogLevelWarn:
		return "\033[33mWARN\033[0m" // Yellow
	case LogLevelError:
		return "\033[31mERROR\033[0m" // Red
	default:
		return level.String()
	}
}

// Global logger instance
var defaultLogger = NewLogger()

// SetDefaultLogger sets the global default logger.
func SetDefaultLogger(l *Logger) {
	defaultLogger = l
}

// GetLogger returns the global default logger.
func GetLogger() *Logger {
	return defaultLogger
}

// Package-level logging functions that use the default logger

func LogDebug(msg string, args ...interface{}) {
	defaultLogger.Debug(msg, args...)
}

func LogInfo(msg string, args ...interface{}) {
	defaultLogger.Info(msg, args...)
}

func LogWarn(msg string, args ...interface{}) {
	defaultLogger.Warn(msg, args...)
}

func LogError(msg string, args ...interface{}) {
	defaultLogger.Error(msg, args...)
}

// InitLogger initializes the default logger based on environment variables.
func InitLogger() {
	level := LogLevelInfo
	if lvl := os.Getenv("AMUX_LOG_LEVEL"); lvl != "" {
		level = ParseLogLevel(lvl)
	}

	colorize := true
	if os.Getenv("NO_COLOR") != "" || os.Getenv("AMUX_NO_COLOR") != "" {
		colorize = false
	}

	var output io.Writer = os.Stderr
	if logFile := os.Getenv("AMUX_LOG_FILE"); logFile != "" {
		if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			output = io.MultiWriter(os.Stderr, f)
		}
	}

	defaultLogger = NewLogger(
		WithLevel(level),
		WithOutput(output),
		WithColor(colorize),
		WithPrefix("amux"),
	)
}
