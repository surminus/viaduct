package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
	t.Parallel()

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
