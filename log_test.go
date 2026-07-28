package viaduct

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInfoWriter(t *testing.T) {
	orig := Cli.Stdout
	defer func() { Cli.Stdout = orig }()

	Cli.Stdout = false
	assert.Equal(t, os.Stderr, infoWriter())

	Cli.Stdout = true
	assert.Equal(t, os.Stdout, infoWriter())
}

func TestEnvDuration(t *testing.T) {
	const name = "VIADUCT_RESOURCE_TIMEOUT_TEST"

	t.Run("unset means no timeout", func(t *testing.T) {
		os.Unsetenv(name)
		assert.Zero(t, envDuration(name))
	})

	t.Run("parsed", func(t *testing.T) {
		t.Setenv(name, "90s")
		assert.Equal(t, 90*time.Second, envDuration(name))

		t.Setenv(name, "-1s")
		assert.Equal(t, -time.Second, envDuration(name))
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
