package viaduct

import (
	"os"
	"testing"

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
