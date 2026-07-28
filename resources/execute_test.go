package resources

import (
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

	// A lock key implies a lock, and narrows it to that domain
	keyed := (&Execute{Command: "true", LockKey: viaduct.PackageLock}).Params()
	assert.True(t, keyed.GlobalLock)
	assert.Equal(t, viaduct.PackageLock, keyed.LockKey)

	// Without a key the lock applies against every other lock holder
	assert.Empty(t, ExecLocked("true").Params().LockKey)
}
