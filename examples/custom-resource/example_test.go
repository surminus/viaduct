package example

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct/viaducttest"
)

func TestMotd(t *testing.T) {
	t.Run("writes the message and refreshes", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/etc"), 0o755))
		env.Runner.On("run-parts", "/etc/update-motd.d")

		log, err := env.Apply(env.Context(), &Motd{Message: "hello", Refresh: []string{"run-parts", "/etc/update-motd.d"}})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "hello\n", env.ReadFile("/etc/motd"))
		assert.True(t, env.Runner.Ran("run-parts", "/etc/update-motd.d"))
	})

	t.Run("already right is a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/etc/motd", "hello\n")

		log, err := env.Apply(env.Context(), &Motd{Message: "hello", Refresh: []string{"run-parts", "/etc/update-motd.d"}})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("dry run changes nothing", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/etc/motd", "old\n")

		log, err := env.Apply(env.DryRunContext(), &Motd{Message: "hello"})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "old\n", env.ReadFile("/etc/motd"))
	})

	t.Run("message is required", func(t *testing.T) {
		env := viaducttest.NewEnv(t)

		_, err := env.Apply(env.Context(), &Motd{})
		assert.ErrorContains(t, err, "required parameter: Message")
	})
}
