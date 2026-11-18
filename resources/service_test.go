package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct/viaducttest"
)

// Managing services requires root and systemd, so we only test validation
// here. See package_test.go for thoughts on acceptance testing.

func TestServicePreflightChecks(t *testing.T) {
	t.Run("requires name", func(t *testing.T) {
		s := &Service{}

		err := s.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Name")
	})

	t.Run("invalid action", func(t *testing.T) {
		s := &Service{Name: "docker", Action: "reload"}

		err := s.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "action must be one of start, stop or restart: reload")
	})

	t.Run("enable and disable conflict", func(t *testing.T) {
		s := &Service{Name: "docker", Enable: true, Disable: true}

		err := s.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "cannot set both Enable and Disable")
	})

	t.Run("requires something to do", func(t *testing.T) {
		s := &Service{Name: "docker"}

		err := s.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "requires one of Action, Enable, Disable or DaemonReload")
	})
}

func TestServiceHelpers(t *testing.T) {
	assert.Equal(t, &Service{Name: "docker", Action: "start"}, StartService("docker"))
	assert.Equal(t, &Service{Name: "docker", Action: "stop"}, StopService("docker"))
	assert.Equal(t, &Service{Name: "docker", Action: "restart"}, RestartService("docker"))
	assert.Equal(t, &Service{Name: "docker", Enable: true}, EnableService("docker"))
	assert.Equal(t, &Service{Name: "docker", Disable: true}, DisableService("docker"))
}

func TestServiceOperationName(t *testing.T) {
	assert.Equal(t, "Restart", (&Service{Name: "docker", Action: "restart"}).OperationName())
	assert.Equal(t, "Enable", (&Service{Name: "docker", Enable: true}).OperationName())
	assert.Equal(t, "Disable+Stop", (&Service{Name: "docker", Disable: true, Action: "stop"}).OperationName())
	assert.Equal(t, "DaemonReload+Restart", (&Service{Name: "docker", DaemonReload: true, Action: "restart"}).OperationName())
}

func TestServiceDaemonReload(t *testing.T) {
	t.Parallel()

	show := []string{"show", "--property=NeedDaemonReload", "--value", "docker"}

	t.Run("reloads before restarting when the unit changed", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", show...).Stdout("yes\n")
		env.Runner.On("systemctl", "daemon-reload")
		env.Runner.On("systemctl", "restart", "docker")

		log, err := env.Apply(env.Context(), &Service{Name: "docker", Action: "restart", DaemonReload: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{
			"systemctl show --property=NeedDaemonReload --value docker",
			"systemctl daemon-reload",
			"systemctl restart docker",
		}, env.Runner.Commands())
	})

	t.Run("a noop when the unit has not changed", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", show...).Stdout("no\n")

		log, err := env.Apply(env.Context(), &Service{Name: "docker", DaemonReload: true})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"systemctl show --property=NeedDaemonReload --value docker"}, env.Runner.Commands())
	})

	t.Run("dry run checks but does not reload", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", show...).Stdout("yes\n")

		log, err := env.Apply(env.DryRunContext(), &Service{Name: "docker", DaemonReload: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.False(t, env.Runner.Ran("systemctl", "daemon-reload"))
	})
}

func TestServiceRun(t *testing.T) {
	t.Parallel()

	t.Run("enable a disabled service", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "is-enabled", "docker").Stdout("disabled\n").Exit(1)
		env.Runner.On("systemctl", "enable", "docker")

		log, err := env.Apply(env.Context(), EnableService("docker"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"systemctl is-enabled docker", "systemctl enable docker"}, env.Runner.Commands())
	})

	t.Run("enable is a noop when enabled", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "is-enabled", "docker").Stdout("enabled\n")

		log, err := env.Apply(env.Context(), EnableService("docker"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"systemctl is-enabled docker"}, env.Runner.Commands())
	})

	t.Run("static units are left alone", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "is-enabled", "docker").Stdout("static\n")

		log, err := env.Apply(env.Context(), DisableService("docker"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"systemctl is-enabled docker"}, env.Runner.Commands())
	})

	t.Run("masked cannot be enabled, even in a dry run", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "is-enabled", "docker").Stdout("masked\n").Exit(1)

		_, err := env.Apply(env.DryRunContext(), EnableService("docker"))
		assert.EqualError(t, err, "cannot enable masked service docker, unmask it first")
	})

	t.Run("dry run enable checks but does not enable", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "is-enabled", "docker").Stdout("disabled\n").Exit(1)

		log, err := env.Apply(env.DryRunContext(), EnableService("docker"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"systemctl is-enabled docker"}, env.Runner.Commands())
	})

	t.Run("dry run reports an enabled service as a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "is-enabled", "docker").Stdout("enabled\n")

		log, err := env.Apply(env.DryRunContext(), EnableService("docker"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("start an inactive service", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "is-active", "--quiet", "docker").Exit(3)
		env.Runner.On("systemctl", "start", "docker")

		log, err := env.Apply(env.Context(), StartService("docker"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"systemctl is-active --quiet docker", "systemctl start docker"}, env.Runner.Commands())
	})

	t.Run("start is a noop when active", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "is-active", "--quiet", "docker")

		log, err := env.Apply(env.Context(), StartService("docker"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"systemctl is-active --quiet docker"}, env.Runner.Commands())
	})

	t.Run("stop is a noop when inactive", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "is-active", "--quiet", "docker").Exit(3)

		log, err := env.Apply(env.Context(), StopService("docker"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"systemctl is-active --quiet docker"}, env.Runner.Commands())
	})

	t.Run("restart always runs", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "restart", "docker")

		log, err := env.Apply(env.Context(), RestartService("docker"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"systemctl restart docker"}, env.Runner.Commands())
	})

	t.Run("dry run start checks but does not start", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "is-active", "--quiet", "docker").Exit(3)

		log, err := env.Apply(env.DryRunContext(), StartService("docker"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"systemctl is-active --quiet docker"}, env.Runner.Commands())
	})

	t.Run("dry run restart runs nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), RestartService("docker"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("systemctl failing fails the resource", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("systemctl", "restart", "docker").Exit(1)

		_, err := env.Apply(env.Context(), RestartService("docker"))
		assert.ErrorContains(t, err, "systemctl restart failed for docker")
	})
}
