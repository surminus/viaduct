package viaduct

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder is a handler that keeps every event
type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) Handle(_ context.Context, e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, e)
}

func (r *recorder) all() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]Event(nil), r.events...)
}

func TestLogger(t *testing.T) {
	t.Run("events carry the resource and fields", func(t *testing.T) {
		rec := &recorder{}
		l := NewLogger(rec, "File", "Create")

		l.Info("created", "path", "/tmp/foo", "mode", 0o644)
		l.Noop("up-to-date")
		l.Warn("odd")
		l.Error("failed", "error", "boom")

		events := rec.all()
		require.Len(t, events, 4)

		assert.Equal(t, LevelChanged, events[0].Level)
		assert.Equal(t, ResourceKind("File"), events[0].Kind)
		assert.Equal(t, "Create", events[0].Operation)
		assert.Equal(t, "created", events[0].Message)
		assert.Equal(t, []Field{{"path", "/tmp/foo"}, {"mode", 0o644}}, events[0].Fields)
		assert.False(t, events[0].Time.IsZero())

		assert.Equal(t, LevelNoop, events[1].Level)
		assert.Equal(t, LevelWarn, events[2].Level)
		assert.Equal(t, LevelError, events[3].Level)

		v, ok := events[3].Field("error")
		assert.True(t, ok)
		assert.Equal(t, "boom", v)

		_, ok = events[3].Field("missing")
		assert.False(t, ok)
	})

	t.Run("odd fields", func(t *testing.T) {
		assert.Equal(t, []Field{{"a", 1}, {"b", ""}}, fieldsOf([]any{"a", 1, "b"}))
		assert.Equal(t, []Field{{"1", 2}}, fieldsOf([]any{1, 2}))
		assert.Nil(t, fieldsOf(nil))
	})
}

func TestNilLogger(t *testing.T) {
	var l *Logger

	assert.NotPanics(t, func() {
		l.Info("a")
		l.Noop("b")
		l.Warn("c")
		l.Error("d")
		l.progress("e")
	})
}

func TestLevelString(t *testing.T) {
	assert.Equal(t, "changed", LevelChanged.String())
	assert.Equal(t, "noop", LevelNoop.String())
	assert.Equal(t, "output", LevelOutput.String())
	assert.Equal(t, "level(99)", Level(99).String())
}

func TestTextHandler(t *testing.T) {
	orig := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = orig }()

	// The tags are coloured at package load, so NoColor cannot be relied on
	strip := func(s string) string {
		for _, code := range []string{"\x1b[32m", "\x1b[34;2m", "\x1b[33m", "\x1b[31m", "\x1b[0m"} {
			s = strings.ReplaceAll(s, code, "")
		}
		return s
	}

	emit := func(o TextOptions) (string, string) {
		var out, errs bytes.Buffer
		o.Out, o.Err = &out, &errs

		l := NewLogger(NewTextHandler(o), "File", "Create")
		l.Info("created", "path", "/home/user/.bashrc")
		l.Noop("up-to-date", "path", "/home/user/.bashrc")
		l.Warn("odd", "msg", "has spaces")
		l.Error("command failed", "command", "echo hello")
		l.progress("started")

		return strip(out.String()), strip(errs.String())
	}

	t.Run("default", func(t *testing.T) {
		out, errs := emit(TextOptions{})

		assert.Empty(t, out)
		assert.Equal(t, strings.Join([]string{
			"  OK  File [Create]          created path=/home/user/.bashrc",
			"  --  File [Create]          up-to-date path=/home/user/.bashrc",
			"WARN  File [Create]          odd msg=\"has spaces\"",
			"FAIL  File [Create]          command failed command=\"echo hello\"",
			"  OK  File [Create]          started",
			"",
		}, "\n"), errs)
	})

	t.Run("stdout", func(t *testing.T) {
		out, errs := emit(TextOptions{Stdout: true})

		assert.Equal(t, strings.Join([]string{
			"  OK  File [Create]          created path=/home/user/.bashrc",
			"  --  File [Create]          up-to-date path=/home/user/.bashrc",
			"  OK  File [Create]          started",
			"",
		}, "\n"), out)
		assert.Equal(t, strings.Join([]string{
			"WARN  File [Create]          odd msg=\"has spaces\"",
			"FAIL  File [Create]          command failed command=\"echo hello\"",
			"",
		}, "\n"), errs)
	})

	t.Run("quiet", func(t *testing.T) {
		out, errs := emit(TextOptions{Quiet: true})

		assert.Empty(t, out)
		assert.Equal(t, strings.Join([]string{
			"WARN  File [Create]          odd msg=\"has spaces\"",
			"FAIL  File [Create]          command failed command=\"echo hello\"",
			"",
		}, "\n"), errs)
	})
}

func TestJSONCollector(t *testing.T) {
	c := newJSONCollector()

	h := serialise(c)
	r := &Resource{ResourceID: "a", ResourceKind: "File", Attributes: newTestResource("a")}

	l := newResourceLogger(context.Background(), h, r, "Create")
	l.Info("created", "path", "/tmp/a")
	l.Noop("up-to-date")
	NewLogger(h, "Viaduct", "Run").progress("dropped")

	got := c.resources(map[ResourceID]Resource{"a": {ResourceID: "a", ResourceKind: "File", Status: StatusSuccess, Attributes: newTestResource("a")}})
	require.Len(t, got, 1)
	assert.Equal(t, []LogEntry{
		{Level: "OK", Message: "created", Fields: map[string]string{"path": "/tmp/a"}},
		{Level: "NOOP", Message: "up-to-date"},
	}, got[0].Log)
}

func TestEnvDuration(t *testing.T) {
	const name = "VIADUCT_RESOURCE_TIMEOUT_TEST"

	t.Run("unset", func(t *testing.T) {
		os.Unsetenv(name)
		d, ok, err := envDuration(name)
		assert.NoError(t, err)
		assert.False(t, ok)
		assert.Zero(t, d)
	})

	t.Run("parsed", func(t *testing.T) {
		t.Setenv(name, "90s")
		d, ok, err := envDuration(name)
		assert.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, 90*time.Second, d)

		t.Setenv(name, "-1s")
		d, _, err = envDuration(name)
		assert.NoError(t, err)
		assert.Equal(t, -time.Second, d)
	})

	t.Run("unparseable is an error", func(t *testing.T) {
		t.Setenv(name, "soon")
		_, _, err := envDuration(name)
		assert.EqualError(t, err, name+" is not a duration: soon")
	})
}

func TestEnvBool(t *testing.T) {
	const name = "VIADUCT_STDOUT_TEST"

	t.Run("unset", func(t *testing.T) {
		os.Unsetenv(name)
		assert.False(t, envBool(name))
	})

	t.Run("truthy", func(t *testing.T) {
		for _, v := range []string{"1", "t", "true", "TRUE"} {
			t.Setenv(name, v)
			assert.True(t, envBool(name), v)
		}
	})

	t.Run("falsy", func(t *testing.T) {
		for _, v := range []string{"0", "f", "false", "FALSE"} {
			t.Setenv(name, v)
			assert.False(t, envBool(name), v)
		}
	})

	t.Run("unparseable", func(t *testing.T) {
		t.Setenv(name, "nonsense")
		assert.False(t, envBool(name))
	})
}
