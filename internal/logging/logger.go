package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Level represents log severity
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging
type Logger struct {
	mu       sync.Mutex
	writer   io.Writer
	level    Level
	enabled  bool
	filePath string
}

var defaultLogger *Logger

const (
	logDateLayout          = "2006-01-02"
	logPrefix              = "amux-"
	logSuffix              = ".log"
	defaultRetentionDays   = 14
	logRetentionEnvVarName = "AMUX_LOG_RETENTION_DAYS"
)

// Initialize sets up the default logger
func Initialize(logDir string, level Level) error {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	retentionDays := logRetentionDays()
	if retentionDays > 0 {
		_ = pruneOldLogs(logDir, retentionDays)
	}

	logPath := filepath.Join(logDir, fmt.Sprintf("%s%s%s", logPrefix, time.Now().Format(logDateLayout), logSuffix))
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	defaultLogger = &Logger{
		writer:   file,
		level:    level,
		enabled:  true,
		filePath: logPath,
	}

	return nil
}

func logRetentionDays() int {
	raw := strings.TrimSpace(os.Getenv(logRetentionEnvVarName))
	if raw == "" {
		return defaultRetentionDays
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return defaultRetentionDays
	}
	return value
}

func pruneOldLogs(logDir string, retentionDays int) error {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, logPrefix) || !strings.HasSuffix(name, logSuffix) {
			continue
		}
		dateStr := strings.TrimSuffix(strings.TrimPrefix(name, logPrefix), logSuffix)
		day, err := time.ParseInLocation(logDateLayout, dateStr, time.Local)
		if err != nil {
			continue
		}
		if day.Before(cutoff) {
			_ = os.Remove(filepath.Join(logDir, name))
		}
	}
	return nil
}

// SetEnabled enables or disables logging
func SetEnabled(enabled bool) {
	if defaultLogger != nil {
		defaultLogger.mu.Lock()
		defaultLogger.enabled = enabled
		defaultLogger.mu.Unlock()
	}
}

// log writes a log entry
func log(level Level, format string, args ...interface{}) {
	if defaultLogger == nil {
		return
	}

	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()

	if !defaultLogger.enabled || level < defaultLogger.level {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] %s: %s\n", timestamp, level.String(), msg)

	_, _ = defaultLogger.writer.Write([]byte(line))
}

// Debug logs a debug message
func Debug(format string, args ...interface{}) {
	log(LevelDebug, format, args...)
}

// Info logs an info message
func Info(format string, args ...interface{}) {
	log(LevelInfo, format, args...)
}

// Warn logs a warning message
func Warn(format string, args ...interface{}) {
	log(LevelWarn, format, args...)
}

// Error logs an error message
func Error(format string, args ...interface{}) {
	log(LevelError, format, args...)
}

// WithError logs an error with context
func WithError(err error, context string) {
	if err != nil {
		log(LevelError, "%s: %v", context, err)
	}
}

// Close closes the log file
func Close() error {
	if defaultLogger != nil && defaultLogger.writer != nil {
		if closer, ok := defaultLogger.writer.(io.Closer); ok {
			return closer.Close()
		}
	}
	return nil
}

// GetLogPath returns the current log file path
func GetLogPath() string {
	if defaultLogger != nil {
		return defaultLogger.filePath
	}
	return ""
}
