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
