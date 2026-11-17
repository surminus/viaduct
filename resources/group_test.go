package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

// Creating groups requires root, so we only test validation and helpers
// here. See package_test.go for thoughts on acceptance testing.

func TestGroupPreflightChecks(t *testing.T) {
	t.Run("requires name", func(t *testing.T) {
		g := &Group{}

		err := g.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Name")
	})
}

func TestSystemGroup(t *testing.T) {
	g := SystemGroup("node_exporter")

	assert.Equal(t, "node_exporter", g.Name)
	assert.True(t, g.System)
}

func TestGroupOperationName(t *testing.T) {
	assert.Equal(t, "Create", (&Group{Name: "test"}).OperationName())
	assert.Equal(t, "Delete", (&Group{Name: "test", Delete: true}).OperationName())
}

func TestGroupParams(t *testing.T) {
	params := (&Group{Name: "test"}).Params()

	assert.True(t, params.GlobalLock)
	assert.Equal(t, viaduct.PasswdLock, params.LockKey)
}

func TestGroupExistingGIDConflict(t *testing.T) {
	// The root group exists everywhere with gid 0, so asking for a different
	// gid has to be an error rather than a silent noop
	g := &Group{Name: "root", GID: 12345}

	err := g.create(testCtx, testLogger)
	assert.EqualError(t, err, "group root exists with gid 0, not 12345")
}

// absentName is a user and group name that does not exist on the host. The
// tests use it for the change path and root for the noop path.
const absentName = "viaducttest-absent"

func TestGroupRun(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("groupadd", "-r", "-g", "4242", absentName)

		log, err := env.Apply(env.Context(), &Group{Name: absentName, GID: 4242, System: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"groupadd -r -g 4242 " + absentName}, env.Runner.Commands())
	})

	t.Run("existing group is a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.Context(), &Group{Name: "root"})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("dry run create runs nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), &Group{Name: absentName})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("dry run still finds a gid conflict", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		_, err := env.Apply(env.DryRunContext(), &Group{Name: "root", GID: 12345})
		assert.ErrorContains(t, err, "group root exists with gid 0, not 12345")
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("groupdel", "root")

		log, err := env.Apply(env.Context(), &Group{Name: "root", Delete: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"groupdel root"}, env.Runner.Commands())
	})

	t.Run("delete is a noop when absent", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.Context(), &Group{Name: absentName, Delete: true})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("dry run delete runs nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), &Group{Name: "root", Delete: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})
}
