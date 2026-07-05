package logging

import (
	"fmt"
	"io"
	"log/slog"
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
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return err
	}

	retentionDays := logRetentionDays()
	if retentionDays > 0 {
		if err := pruneOldLogs(logDir, retentionDays); err != nil {
			slog.Debug("log pruning failed", "error", err)
		}
	}

	logName := fmt.Sprintf("%s%s%s", logPrefix, time.Now().Format(logDateLayout), logSuffix)
	logPath := filepath.Join(logDir, logName)
	file, err := openLogFileInDir(logDir, logName)
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

func openLogFileInDir(logDir, logName string) (*os.File, error) {
	root, err := os.OpenRoot(logDir)
	if err != nil {
		return nil, fmt.Errorf("open log directory: %w", err)
	}
	file, openErr := root.OpenFile(logName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	closeErr := root.Close()
	if openErr != nil {
		if closeErr != nil {
			return nil, fmt.Errorf("open log file: %w; close log directory: %w", openErr, closeErr)
		}
		return nil, fmt.Errorf("open log file: %w", openErr)
	}
	if closeErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("close log directory: %w", closeErr)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("set log file permissions: %w", err)
	}
	return file, nil
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

// ParseLevel maps a level name (debug/info/warn/error, case-insensitive and
// trimmed) to a Level. The bool is false for unrecognized input so callers can
// fall back to a default.
func ParseLevel(name string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return LevelDebug, true
	case "info":
		return LevelInfo, true
	case "warn":
		return LevelWarn, true
	case "error":
		return LevelError, true
	default:
		return LevelInfo, false
	}
}

// log writes a log entry
func log(level Level, format string, args ...any) {
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
func Debug(format string, args ...any) {
	log(LevelDebug, format, args...)
}

// Info logs an info message
func Info(format string, args ...any) {
	log(LevelInfo, format, args...)
}

// Warn logs a warning message
func Warn(format string, args ...any) {
	log(LevelWarn, format, args...)
}

// Error logs an error message
func Error(format string, args ...any) {
	log(LevelError, format, args...)
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
