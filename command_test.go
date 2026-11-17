package viaduct

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecRunner(t *testing.T) {
	ctx := context.Background()

	t.Run("output and exit status", func(t *testing.T) {
		rec := &recorder{}
		log := NewLogger(rec, "Test", "Test")

		out, err := Command{Name: "sh", Args: []string{"-c", "echo out; echo err >&2; exit 3"}}.Output(ctx, log)

		assert.Equal(t, "out\n", string(out))
		code, ok := ExitCode(err)
		assert.True(t, ok)
		assert.Equal(t, 3, code)
		assert.Equal(t, []string{"err"}, rec.messages(LevelOutput))
	})

	t.Run("working directory, environment and stdin", func(t *testing.T) {
		dir := t.TempDir()

		out, err := Command{
			Name:  "sh",
			Args:  []string{"-c", `pwd; echo "$VIADUCT_TEST"; cat`},
			Dir:   dir,
			Env:   []string{"VIADUCT_TEST=set"},
			Stdin: strings.NewReader("input"),
		}.Output(ctx, NewLogger(nil, "Test", "Test"))
		require.NoError(t, err)

		resolved, err := filepath.EvalSymlinks(dir)
		require.NoError(t, err)
		assert.Equal(t, resolved+"\nset\ninput", string(out))
	})

	t.Run("a missing program is not an exit status", func(t *testing.T) {
		err := Command{Name: "viaduct-does-not-exist"}.Run(ctx, NewLogger(nil, "Test", "Test"))
		require.Error(t, err)

		_, ok := ExitCode(err)
		assert.False(t, ok)
	})

	t.Run("cancelling stops the command and its children", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "marker")

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := Command{Name: "sh", Args: []string{"-c", "(sleep 1; touch " + marker + ") & sleep 30"}}.Run(ctx, NewLogger(nil, "Test", "Test"))

		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(start), 10*time.Second)

		time.Sleep(1500 * time.Millisecond)
		assert.NoFileExists(t, marker, "the child of the shell was stopped too")
	})

	t.Run("children that ignore SIGTERM are killed", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "marker")

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		// The child ignores SIGTERM, and redirects its output so nothing
		// holds the pipe open
		script := "(trap '' TERM; sleep 4; touch " + marker + ") >/dev/null 2>&1 & sleep 30"
		err := Command{Name: "sh", Args: []string{"-c", script}}.Run(ctx, NewLogger(nil, "Test", "Test"))
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		time.Sleep(5 * time.Second)
		assert.NoFileExists(t, marker, "the child that ignored SIGTERM was killed")
	})

	t.Run("a background process holding the output open does not hang", func(t *testing.T) {
		start := time.Now()

		err := Command{Name: "sh", Args: []string{"-c", "sleep 30 &"}}.Run(ctx, NewLogger(nil, "Test", "Test"))
		require.NoError(t, err)

		assert.Less(t, time.Since(start), 10*time.Second)
	})

	t.Run("output stops being collected after the wait delay", func(t *testing.T) {
		start := time.Now()

		// Output writes to a bytes.Buffer, which has no lock, so the race
		// detector catches a write after Output has returned
		out, err := Command{Name: "sh", Args: []string{"-c", "echo first; (sleep 3; echo late) &"}}.Output(ctx, nil)
		require.NoError(t, err)

		elapsed := time.Since(start)
		assert.GreaterOrEqual(t, elapsed, commandWaitDelay)
		assert.Less(t, elapsed, commandWaitDelay+time.Second)

		time.Sleep(2 * time.Second)
		assert.Equal(t, "first\n", string(out))
	})

	t.Run("a command is given its grace to stop after SIGTERM", func(t *testing.T) {
		withCancelGrace(t, 5*time.Second)

		marker := filepath.Join(t.TempDir(), "marker")

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		// Takes longer to clean up than the delay for output after exiting
		script := "trap 'sleep 2.5; touch " + marker + "; exit 1' TERM; while :; do sleep 0.05; done"
		err := Command{Name: "sh", Args: []string{"-c", script}}.Run(ctx, nil)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.FileExists(t, marker, "the command finished cleaning up")
	})

	t.Run("a command that exits 0 on SIGTERM has still failed", func(t *testing.T) {
		withCancelGrace(t, 5*time.Second)

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		err := Command{Name: "sh", Args: []string{"-c", "trap 'exit 0' TERM; while :; do sleep 0.05; done"}}.Run(ctx, nil)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("a command that ignores SIGTERM is killed after its grace", func(t *testing.T) {
		withCancelGrace(t, 200*time.Millisecond)

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := Command{Name: "sh", Args: []string{"-c", "trap '' TERM; while :; do sleep 0.05; done"}}.Run(ctx, nil)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(start), 5*time.Second)
	})
}

// withCancelGrace sets how long commands have to stop after SIGTERM for the
// rest of the test
func withCancelGrace(t *testing.T, d time.Duration) {
	orig := commandCancelGrace
	commandCancelGrace = d

	t.Cleanup(func() { commandCancelGrace = orig })
}

func TestExitCode(t *testing.T) {
	code, ok := ExitCode(&ExitError{Code: 2})
	assert.True(t, ok)
	assert.Equal(t, 2, code)

	_, ok = ExitCode(errors.New("other"))
	assert.False(t, ok)

	_, ok = ExitCode(nil)
	assert.False(t, ok)

	assert.EqualError(t, &ExitError{Code: 2}, "exit status 2")
}
