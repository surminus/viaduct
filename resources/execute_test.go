package resources

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

func newTestExecute(t *testing.T) *Execute {
	e := &Execute{Command: "true"}

	err := e.PreflightChecks(testCtx, testLogger)
	if err != nil {
		t.Fatal(err)
	}

	return e
}

func TestExecute(t *testing.T) {
	t.Run("without error", func(t *testing.T) {
		e := newTestExecute(t)

		err := e.Run(testCtx, testLogger)
		assert.NoError(t, err)
	})

	t.Run("with error", func(t *testing.T) {
		e := newTestExecute(t)
		e.Command = "false"

		err := e.Run(testCtx, testLogger)
		assert.ErrorContains(t, err, "exit status 1")
	})

	t.Run("a failed command does not mark the resource changed", func(t *testing.T) {
		log := viaduct.NewLogger(nil, "Execute", "Run")

		require.Error(t, Exec("false").Run(testCtx, log))
		assert.False(t, log.Changed())
	})

	t.Run("a dry run logs the command without running it", func(t *testing.T) {
		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), Exec("false"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("runs unless in the working directory", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "done"), nil, 0o600))

		e := ExecUnless("false", "test -f done")
		e.WorkingDirectory = dir

		assert.NoError(t, e.Run(testCtx, testLogger))
	})
}

func TestExecuteLock(t *testing.T) {
	assert.False(t, Exec("true").Params().GlobalLock)
	assert.True(t, (&Execute{Command: "true", Lock: true}).Params().GlobalLock)
	assert.True(t, ExecLocked("true").Params().GlobalLock)
	assert.True(t, ExecArgsLocked("true").Params().GlobalLock)

	// A lock key implies a lock, and narrows it to that domain
	keyed := (&Execute{Command: "true", LockKey: viaduct.PackageLock}).Params()
	assert.True(t, keyed.GlobalLock)
	assert.Equal(t, viaduct.PackageLock, keyed.LockKey)

	// Without a key the lock applies against every other lock holder
	assert.Empty(t, ExecLocked("true").Params().LockKey)
}

func TestExecuteArgs(t *testing.T) {
	t.Run("runs without a shell", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file with spaces; and metacharacters")

		e := ExecArgs("touch", path)
		if err := e.PreflightChecks(testCtx, testLogger); err != nil {
			t.Fatal(err)
		}

		assert.NoError(t, e.Run(testCtx, testLogger))
		assert.FileExists(t, path)
	})

	t.Run("returns the command failure", func(t *testing.T) {
		e := ExecArgs("false")
		if err := e.PreflightChecks(testCtx, testLogger); err != nil {
			t.Fatal(err)
		}

		assert.Error(t, e.Run(testCtx, testLogger))
	})

	t.Run("describes itself with the full command", func(t *testing.T) {
		assert.Equal(t, "touch /tmp/test", ExecArgs("touch", "/tmp/test").Description())
	})
}

func TestExecutePreflightChecks(t *testing.T) {
	t.Run("requires a command", func(t *testing.T) {
		err := (&Execute{}).PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Command")
	})

	t.Run("rejects both command and args", func(t *testing.T) {
		err := (&Execute{Command: "true", Args: []string{"true"}}).PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "cannot set both Command and Args")
	})

	t.Run("accepts args alone", func(t *testing.T) {
		assert.NoError(t, (&Execute{Args: []string{"true"}}).PreflightChecks(testCtx, testLogger))
	})
}

func TestExecuteOutput(t *testing.T) {
	t.Run("each line of output is an event", func(t *testing.T) {
		var events []viaduct.Event
		log := viaduct.NewLogger(viaduct.HandlerFunc(func(_ context.Context, e viaduct.Event) {
			events = append(events, e)
		}), "Execute", "Run")

		e := Exec("echo one; echo two >&2; printf three")
		require.NoError(t, e.Run(testCtx, log))

		var lines []string
		streams := map[string]any{}
		for _, ev := range events {
			if ev.Level == viaduct.LevelOutput {
				lines = append(lines, ev.Message)
				streams[ev.Message], _ = ev.Field("stream")
			}
		}

		assert.ElementsMatch(t, []string{"one", "two", "three"}, lines)
		assert.Equal(t, viaduct.Stdout, streams["one"])
		assert.Equal(t, viaduct.Stderr, streams["two"])
	})

	t.Run("a background process holding the output open does not hang", func(t *testing.T) {
		start := time.Now()

		e := Exec("sleep 30 &")
		require.NoError(t, e.Run(testCtx, testLogger))

		assert.Less(t, time.Since(start), 10*time.Second)
	})
}
