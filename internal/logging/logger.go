package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

const (
	LevelDebug = "DEBUG"
	LevelInfo  = "INFO"
	LevelWarn  = "WARN"
	LevelError = "ERROR"
)

// Logger wraps the standard logger with level filtering.
type Logger struct {
	base  *log.Logger
	level string
}

// NewLogger creates a logger that writes to stdout and honors the given level.
func NewLogger(level string, output io.Writer) *Logger {
	if output == nil {
		output = os.Stdout
	}
	return &Logger{
		base:  log.New(output, "", log.LstdFlags|log.Lmsgprefix),
		level: strings.ToUpper(strings.TrimSpace(level)),
	}
}

// Debug logs a debug message when the enabled level is DEBUG.
func (l *Logger) Debug(msg string, v ...any) {
	if l.enabled(LevelDebug) {
		l.base.Printf("DEBUG: %s", fmtArgs(msg, v...))
	}
}

// Info logs an informational message when the enabled level is DEBUG or INFO.
func (l *Logger) Info(msg string, v ...any) {
	if l.enabled(LevelInfo) {
		l.base.Printf("INFO: %s", fmtArgs(msg, v...))
	}
}

// Warn logs a warning message at WARN or higher.
func (l *Logger) Warn(msg string, v ...any) {
	if l.enabled(LevelWarn) {
		l.base.Printf("WARN: %s", fmtArgs(msg, v...))
	}
}

// Error logs an error message at ERROR.
func (l *Logger) Error(msg string, v ...any) {
	if l.enabled(LevelError) {
		l.base.Printf("ERROR: %s", fmtArgs(msg, v...))
	}
}

func (l *Logger) enabled(level string) bool {
	switch strings.ToUpper(level) {
	case LevelDebug:
		return l.level == LevelDebug
	case LevelInfo:
		return l.level == LevelDebug || l.level == LevelInfo
	case LevelWarn:
		return l.level == LevelDebug || l.level == LevelInfo || l.level == LevelWarn
	case LevelError:
		return true
	default:
		return true
	}
}

func fmtArgs(msg string, v ...any) string {
	if len(v) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, v...)
}
