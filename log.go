package viaduct

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
)

// infoWriter returns the destination for non-error output. By default this is
// STDERR, but the --stdout flag (or VIADUCT_STDOUT) routes it to STDOUT.
// Errors always go to STDERR.
func infoWriter() io.Writer {
	if Cli.Stdout {
		return os.Stdout
	}
	return os.Stderr
}

// LogEntry captures a single structured log call.
type LogEntry struct {
	Level   string            `json:"level"`
	Message string            `json:"msg"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// Logger provides structured Viaduct log output.
type Logger struct {
	// Resource is the resource type, such as File or Git.
	Resource string

	// Action is the operation, such as Create or Delete.
	Action string

	// Quiet suppresses OK and NOOP output, shows WARN and ERR.
	Quiet bool

	// Silent suppresses all output except FATAL.
	Silent bool

	// jsonMode buffers entries instead of printing.
	jsonMode bool

	// entries collects log entries in JSON mode.
	entries []LogEntry
}

// Log emits a user-level message.
func Log(msg string, fields ...string) {
	l := NewLogger("Viaduct", "User")
	if l.Silent || l.Quiet || l.jsonMode {
		return
	}
	l.Info(msg, fields...)
}

func NewLogger(resource, action string) *Logger {
	if Cli.JSON {
		return &Logger{Resource: resource, Action: action, jsonMode: true}
	}

	if Cli.Silent {
		return NewSilentLogger()
	}

	if Cli.Quiet {
		return NewQuietLogger(resource, action)
	}

	return &Logger{
		Resource: resource,
		Action:   action,
	}
}

func NewStandardLogger(resource, action string) *Logger {
	return &Logger{
		Resource: resource,
		Action:   action,
	}
}

func NewQuietLogger(resource, action string) *Logger {
	return &Logger{
		Resource: resource,
		Action:   action,
		Quiet:    true,
	}
}

func NewSilentLogger() *Logger {
	return &Logger{Silent: true}
}

// Entries returns the buffered log entries (for JSON mode).
func (l *Logger) Entries() []LogEntry {
	return l.entries
}

// Info logs that an action was taken. Suppressed in Quiet and Silent modes.
func (l *Logger) Info(msg string, fields ...string) {
	if l.jsonMode {
		l.entries = append(l.entries, newEntry("OK", msg, fields))
		return
	}

	if l.Silent || l.Quiet {
		return
	}

	fmt.Fprintln(infoWriter(), formatLine(okTag, l.Resource, l.Action, msg, fields))
}

// Noop logs that a resource is already in the desired state.
// Suppressed in Quiet and Silent modes.
func (l *Logger) Noop(msg string, fields ...string) {
	if l.jsonMode {
		l.entries = append(l.entries, newEntry("NOOP", msg, fields))
		return
	}

	if l.Silent || l.Quiet {
		return
	}

	fmt.Fprintln(infoWriter(), formatLine(noopTag, l.Resource, l.Action, msg, fields))
}

// Warn logs a warning message. Suppressed only in Silent mode.
func (l *Logger) Warn(msg string, fields ...string) {
	if l.jsonMode {
		l.entries = append(l.entries, newEntry("WARN", msg, fields))
		return
	}

	if l.Silent {
		return
	}

	fmt.Fprintln(os.Stderr, formatLine(warnTag, l.Resource, l.Action, msg, fields))
}

// Error logs an error message. Suppressed only in Silent mode.
func (l *Logger) Error(msg string, fields ...string) {
	if l.jsonMode {
		l.entries = append(l.entries, newEntry("ERR", msg, fields))
		return
	}

	if l.Silent {
		return
	}

	fmt.Fprintln(os.Stderr, formatLine(failTag, l.Resource, l.Action, msg, fields))
}

// Fatal logs an error message and exits.
func (l *Logger) Fatal(msg string, fields ...string) {
	if l.jsonMode {
		l.entries = append(l.entries, newEntry("FATAL", msg, fields))
		os.Exit(1)
	}

	if l.Silent {
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, formatLine(failTag, l.Resource, l.Action, msg, fields))
	os.Exit(1)
}

func newEntry(level, msg string, fields []string) LogEntry {
	entry := LogEntry{
		Level:   level,
		Message: msg,
	}

	if len(fields) >= 2 {
		entry.Fields = make(map[string]string)
		for i := 0; i+1 < len(fields); i += 2 {
			entry.Fields[fields[i]] = fields[i+1]
		}
	}

	return entry
}

// formatLine builds a human-readable output line:
//
//	  OK  File [Create] created path=/home/user/.bashrc
//	  --  File [Create] up-to-date path=/home/user/.bashrc
//	FAIL  Execute [Run] command failed command="echo hello"
const resourceActionWidth = 22

func formatLine(tag, resource, action, msg string, fields []string) string {
	var b strings.Builder

	ra := fmt.Sprintf("%s [%s]", resource, action)
	fmt.Fprintf(&b, "%s  %-*s %s", tag, resourceActionWidth, ra, msg)

	for i := 0; i+1 < len(fields); i += 2 {
		fmt.Fprintf(&b, " %s=%s", fields[i], quoteIfNeeded(fields[i+1]))
	}

	return b.String()
}

// quoteIfNeeded wraps a value in double quotes if it contains spaces or
// special characters.
func quoteIfNeeded(v string) string {
	if strings.ContainsAny(v, " \t\n\"=") {
		return fmt.Sprintf("%q", v)
	}
	return v
}

var okTag = color.New(color.FgGreen).Sprintf("%4s", "OK")
var noopTag = color.New(color.FgBlue, color.Faint).Sprintf("%4s", "--")
var warnTag = color.New(color.FgYellow).Sprintf("%4s", "WARN")
var failTag = color.New(color.FgRed).Sprintf("%4s", "FAIL")
