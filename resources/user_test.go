package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

// Creating users requires root, so we only test validation and helpers
// here. See package_test.go for thoughts on acceptance testing.

func TestUserPreflightChecks(t *testing.T) {
	t.Run("requires name", func(t *testing.T) {
		u := &User{}

		err := u.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Name")
	})
}

func TestSystemUser(t *testing.T) {
	u := SystemUser("node_exporter")

	assert.Equal(t, "node_exporter", u.Name)
	assert.True(t, u.System)
	assert.Equal(t, "/bin/false", u.Shell)
}

func TestUserRun(t *testing.T) {
	t.Parallel()

	t.Run("create with a group of its own", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("groupadd", "-g", "4242", absentName)
		env.Runner.On("useradd", "-r", "-u", "4242", "-g", "4242", "-G", "docker,adm", "-s", "/bin/false", absentName)

		u := &User{Name: absentName, UID: 4242, GID: 4242, System: true, Shell: "/bin/false", Groups: []string{"docker", "adm"}}

		log, err := env.Apply(env.Context(), u)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{
			"groupadd -g 4242 " + absentName,
			"useradd -r -u 4242 -g 4242 -G docker,adm -s /bin/false " + absentName,
		}, env.Runner.Commands())
	})

	t.Run("create with the default shell", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("useradd", "-s", "/bin/bash", absentName)

		_, err := env.Apply(env.Context(), &User{Name: absentName})
		require.NoError(t, err)

		assert.Equal(t, []string{"useradd -s /bin/bash " + absentName}, env.Runner.Commands())
	})

	t.Run("existing user is a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.Context(), &User{Name: "root"})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("existing user already in its groups is a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.Context(), &User{Name: "root", Groups: []string{"root"}})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("adds missing groups to an existing user", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("usermod", "-a", "-G", absentName, "root")

		log, err := env.Apply(env.Context(), &User{Name: "root", Groups: []string{"root", absentName}})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"usermod -a -G " + absentName + " root"}, env.Runner.Commands())
	})

	t.Run("dry run create runs nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), &User{Name: absentName, GID: 4242})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
		assert.Equal(t, []string{"created", "created"}, env.Recorder.Messages(viaduct.LevelChanged))
	})

	t.Run("dry run group change runs nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), &User{Name: "root", Groups: []string{absentName}})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})
}
