package resources

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

func newTestExecute(t *testing.T) *Execute {
	e := &Execute{Command: "true"}

	err := e.PreflightChecks(testLogger)
	if err != nil {
		t.Fatal(err)
	}

	return e
}

func TestExecute(t *testing.T) {
	t.Run("without error", func(t *testing.T) {
		e := newTestExecute(t)

		err := e.Run(testLogger)
		assert.NoError(t, err)
	})

	t.Run("with error", func(t *testing.T) {
		e := newTestExecute(t)
		e.Command = "false"

		err := e.Run(testLogger)
		assert.Error(t, err)
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
		if err := e.PreflightChecks(testLogger); err != nil {
			t.Fatal(err)
		}

		assert.NoError(t, e.Run(testLogger))
		assert.FileExists(t, path)
	})

	t.Run("returns the command failure", func(t *testing.T) {
		e := ExecArgs("false")
		if err := e.PreflightChecks(testLogger); err != nil {
			t.Fatal(err)
		}

		assert.Error(t, e.Run(testLogger))
	})

	t.Run("describes itself with the full command", func(t *testing.T) {
		assert.Equal(t, "touch /tmp/test", ExecArgs("touch", "/tmp/test").Description())
	})
}

func TestExecutePreflightChecks(t *testing.T) {
	t.Run("requires a command", func(t *testing.T) {
		err := (&Execute{}).PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Command")
	})

	t.Run("rejects both command and args", func(t *testing.T) {
		err := (&Execute{Command: "true", Args: []string{"true"}}).PreflightChecks(testLogger)
		assert.EqualError(t, err, "cannot set both Command and Args")
	})

	t.Run("accepts args alone", func(t *testing.T) {
		assert.NoError(t, (&Execute{Args: []string{"true"}}).PreflightChecks(testLogger))
	})
}
