package resources

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

func TestCommandsDryRun(t *testing.T) {
	t.Parallel()

	for name, r := range map[string]func() viaduct.ResourceAttributes{
		"execute": func() viaduct.ResourceAttributes { return &Execute{Command: "make install", Unless: "test -f /done"} },
		"shell":   func() viaduct.ResourceAttributes { return &Shell{Script: "make install", Unless: "test -f /done"} },
	} {
		t.Run(name+" runs its Unless guard and nothing else", func(t *testing.T) {
			t.Parallel()

			env := viaducttest.NewEnv(t)
			env.Runner.On("bash", "-c", "test -f /done").Exit(1)

			log, err := env.Apply(env.DryRunContext(), r())
			require.NoError(t, err)

			assert.True(t, log.Changed())
			assert.Equal(t, []string{"bash -c test -f /done"}, env.Runner.Commands())
		})

		t.Run(name+" is a noop in a dry run when Unless succeeds", func(t *testing.T) {
			t.Parallel()

			env := viaducttest.NewEnv(t)
			env.Runner.On("bash", "-c", "test -f /done")

			log, err := env.Apply(env.DryRunContext(), r())
			require.NoError(t, err)

			assert.False(t, log.Changed())
		})
	}

	t.Run("a command that fails to start is an error", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("missing-program", "--flag").Fail(exec.ErrNotFound)

		_, err := env.Apply(env.Context(), ExecArgs("missing-program", "--flag"))
		assert.ErrorIs(t, err, exec.ErrNotFound)
		assert.ErrorContains(t, err, "command failed: missing-program --flag")
	})

	t.Run("a script whose interpreter fails to start is an error", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.OnAny("bash").Fail(exec.ErrNotFound)

		_, err := env.Apply(env.Context(), Bash("echo hello"))
		assert.ErrorIs(t, err, exec.ErrNotFound)
	})
}

func TestFilesDryRun(t *testing.T) {
	t.Parallel()

	t.Run("line writes nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/etc/app.conf", "a=1\n")

		log, err := env.Apply(env.DryRunContext(), ReplaceLine("/etc/app.conf", "^a=", "a=2"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "a=1\n", env.ReadFile("/etc/app.conf"))

		log, err = env.Apply(env.DryRunContext(), AppendLine("/etc/new.conf", "a=1"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.NoFileExists(t, env.Path("/etc/new.conf"))

		log, err = env.Apply(env.DryRunContext(), DeleteLine("/etc/app.conf", "^a="))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "a=1\n", env.ReadFile("/etc/app.conf"))
	})

	t.Run("line already present is a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/etc/app.conf", "a=1\n")

		log, err := env.Apply(env.DryRunContext(), AppendLine("/etc/app.conf", "a=1"))
		require.NoError(t, err)
		assert.False(t, log.Changed())
	})

	t.Run("template writes nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/srv/app.tmpl", "name={{ .name }}\n")
		env.WriteFile("/etc/app.conf", "name=old\n")

		tmpl := &Template{Source: "/srv/app.tmpl", Path: "/etc/app.conf", Variables: map[string]string{"name": "new"}}

		log, err := env.Apply(env.DryRunContext(), tmpl)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "name=old\n", env.ReadFile("/etc/app.conf"))

		log, err = env.Apply(env.DryRunContext(), &Template{Source: "/srv/app.tmpl", Path: "/etc/new.conf", Variables: map[string]string{"name": "new"}})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.NoFileExists(t, env.Path("/etc/new.conf"))
	})
}
