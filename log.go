package viaduct

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// Level is the kind of event a resource or the run reports.
type Level int

const (
	// LevelInfo is progress of the run itself, such as starting or finishing.
	LevelInfo Level = iota

	// LevelOutput is a line of command output. The "stream" field is Stdout
	// or Stderr.
	LevelOutput

	// LevelNoop is a resource finding something already as it should be.
	LevelNoop

	// LevelChanged is a resource changing something, or in a dry run, a
	// change it would have made.
	LevelChanged

	// LevelWarn is something worth knowing that did not stop the resource.
	LevelWarn

	// LevelError is a failure.
	LevelError
)

// String returns the lower case name of the level.
func (l Level) String() string {
	switch l {
	case LevelInfo:
		return "info"
	case LevelOutput:
		return "output"
	case LevelNoop:
		return "noop"
	case LevelChanged:
		return "changed"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return fmt.Sprintf("level(%d)", int(l))
	}
}

// Output streams, as the value of the "stream" field on a LevelOutput event.
const (
	Stdout = "stdout"
	Stderr = "stderr"
)

// Field is a key and value attached to an event.
type Field struct {
	Key   string
	Value any
}

// Event is a single thing a resource or the run reports.
type Event struct {
	Time  time.Time
	Level Level

	// ResourceID is empty for events about the run itself.
	ResourceID ResourceID

	// Kind is the resource kind, such as "File", or "Viaduct" for events
	// about the run itself.
	Kind ResourceKind

	// Operation is what the resource is doing, such as "Create", or "Run"
	// for events about the run itself.
	Operation string

	// Description identifies the resource, such as a file path.
	Description string

	Message string
	Fields  []Field
}

// Field returns the value of the first field with the given key, and whether
// there was one.
func (e Event) Field(key string) (any, bool) {
	for _, f := range e.Fields {
		if f.Key == key {
			return f.Value, true
		}
	}

	return nil, false
}

// Handler receives the events from a run. Handle is not called concurrently
// within a run, so a handler needs its own locking only if shared between
// runs.
type Handler interface {
	Handle(ctx context.Context, e Event)
}

// HandlerFunc adapts a function to a Handler.
type HandlerFunc func(ctx context.Context, e Event)

// Handle calls f.
func (f HandlerFunc) Handle(ctx context.Context, e Event) {
	f(ctx, e)
}

// DiscardHandler drops every event.
var DiscardHandler Handler = HandlerFunc(func(context.Context, Event) {})

// serialHandler stops a handler being called concurrently.
type serialHandler struct {
	mu sync.Mutex
	h  Handler
}

func (s *serialHandler) Handle(ctx context.Context, e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.h.Handle(ctx, e)
}

// serialise wraps a handler to serialise its calls.
func serialise(h Handler) Handler {
	if h == nil {
		return DiscardHandler
	}

	if _, ok := h.(*serialHandler); ok {
		return h
	}

	return &serialHandler{h: h}
}

// Logger reports what a resource does. Apply gives each resource its own.
// Every method of a nil Logger does nothing.
type Logger struct {
	ctx     context.Context
	handler Handler

	id          ResourceID
	kind        ResourceKind
	operation   string
	description string
}

// NewLogger returns a logger that sends events to h, attributed to the given
// kind and operation. Apply creates each resource's logger, so this is for
// testing a resource or using one outside a manifest. A nil handler drops
// every event.
func NewLogger(h Handler, kind, operation string) *Logger {
	return &Logger{
		ctx:       context.Background(),
		handler:   serialise(h),
		kind:      ResourceKind(kind),
		operation: operation,
	}
}

// newResourceLogger returns the logger for one attempt at a resource.
func newResourceLogger(ctx context.Context, h Handler, r *Resource, operation string) *Logger {
	return &Logger{
		ctx:         ctx,
		handler:     h,
		id:          r.ResourceID,
		kind:        r.ResourceKind,
		operation:   operation,
		description: r.Attributes.Description(),
	}
}

// Info logs a change the resource made, or in a dry run, a change it would
// have made.
func (l *Logger) Info(msg string, fields ...any) {
	l.emit(LevelChanged, msg, fields)
}

// Noop logs that something is already as it should be.
func (l *Logger) Noop(msg string, fields ...any) {
	l.emit(LevelNoop, msg, fields)
}

// Warn logs something worth knowing that does not stop the resource.
func (l *Logger) Warn(msg string, fields ...any) {
	l.emit(LevelWarn, msg, fields)
}

// Error logs a failure.
func (l *Logger) Error(msg string, fields ...any) {
	l.emit(LevelError, msg, fields)
}

// progress logs the progress of the run itself.
func (l *Logger) progress(msg string, fields ...any) {
	l.emit(LevelInfo, msg, fields)
}

func (l *Logger) emit(level Level, msg string, kv []any) {
	if l == nil {
		return
	}

	l.handler.Handle(l.ctx, Event{
		Time:        time.Now(),
		Level:       level,
		ResourceID:  l.id,
		Kind:        l.kind,
		Operation:   l.operation,
		Description: l.description,
		Message:     msg,
		Fields:      fieldsOf(kv),
	})
}

// fieldsOf pairs alternating keys and values. Non-string keys are formatted,
// and a key without a value gets an empty one.
func fieldsOf(kv []any) []Field {
	if len(kv) == 0 {
		return nil
	}

	fields := make([]Field, 0, (len(kv)+1)/2)

	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			key = fmt.Sprint(kv[i])
		}

		var value any = ""
		if i+1 < len(kv) {
			value = kv[i+1]
		}

		fields = append(fields, Field{Key: key, Value: value})
	}

	return fields
}

// maxLineLength is the longest line a Logger's writer holds. A longer line
// is logged in pieces of this length.
const maxLineLength = 64 * 1024

// Writer returns a writer that logs each line written to it as a LevelOutput
// event on the given stream, Stdout or Stderr. A line ends at "\n", "\r" or
// "\r\n", and one longer than 64 KiB is logged in pieces. Close it to log a
// final line that has no line break.
func (l *Logger) Writer(stream string) io.WriteCloser {
	if l == nil {
		return nopCloser{io.Discard}
	}

	return &lineWriter{log: l, stream: stream}
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error {
	return nil
}

// lineWriter splits what is written to it into lines, logging each one.
type lineWriter struct {
	log    *Logger
	stream string

	mu  sync.Mutex
	buf []byte

	// afterCR is set when the last line ended in "\r", so a "\n" that
	// follows in the next write completes "\r\n" rather than ending an empty
	// line.
	afterCR bool
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)

	for len(w.buf) > 0 {
		if w.afterCR {
			w.afterCR = false

			if w.buf[0] == '\n' {
				w.buf = w.buf[1:]
				continue
			}
		}

		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}

		w.line(w.buf[:i])
		w.afterCR = w.buf[i] == '\r'
		w.buf = w.buf[i+1:]
	}

	for len(w.buf) >= maxLineLength {
		w.line(w.buf[:maxLineLength])
		w.buf = w.buf[maxLineLength:]
	}

	return len(p), nil
}

func (w *lineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) > 0 {
		w.line(w.buf)
		w.buf = nil
	}

	return nil
}

func (w *lineWriter) line(b []byte) {
	w.log.emit(LevelOutput, string(b), []any{"stream", w.stream})
}
