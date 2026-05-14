package logger

import (
	"fmt"
	"log"
	"os"
	"time"
)

// Level represents log level
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

// Logger is a simple structured logger
type Logger struct {
	level  Level
	logger *log.Logger
}

// New creates a new logger
func New(level Level) *Logger {
	return &Logger{
		level:  level,
		logger: log.New(os.Stdout, "", 0),
	}
}

// formatLog formats log message with timestamp and level
func (l *Logger) formatLog(lvl string, msg string, fields map[string]interface{}) string {
	timestamp := time.Now().Format("2006-01-02T15:04:05.000Z07:00")
	fieldsStr := ""
	for k, v := range fields {
		fieldsStr += fmt.Sprintf(" %s=%v", k, v)
	}
	return fmt.Sprintf("[%s] %s %s%s", timestamp, lvl, msg, fieldsStr)
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields map[string]interface{}) {
	if l.level <= DEBUG {
		l.logger.Println(l.formatLog("DEBUG", msg, fields))
	}
}

// Info logs an info message
func (l *Logger) Info(msg string, fields map[string]interface{}) {
	if l.level <= INFO {
		l.logger.Println(l.formatLog("INFO", msg, fields))
	}
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields map[string]interface{}) {
	if l.level <= WARN {
		l.logger.Println(l.formatLog("WARN", msg, fields))
	}
}

// Error logs an error message
func (l *Logger) Error(msg string, fields map[string]interface{}) {
	if l.level <= ERROR {
		l.logger.Println(l.formatLog("ERROR", msg, fields))
	}
}

// WithFields returns a structured log entry
func (l *Logger) WithFields(fields map[string]interface{}) *Entry {
	return &Entry{
		logger: l,
		fields: fields,
	}
}

// Entry represents a log entry with fields
type Entry struct {
	logger *Logger
	fields map[string]interface{}
}

func (e *Entry) Debug(msg string) {
	e.logger.Debug(msg, e.fields)
}

func (e *Entry) Info(msg string) {
	e.logger.Info(msg, e.fields)
}

func (e *Entry) Warn(msg string) {
	e.logger.Warn(msg, e.fields)
}

func (e *Entry) Error(msg string) {
	e.logger.Error(msg, e.fields)
}
