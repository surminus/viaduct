package resources

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

func TestKernelModule(t *testing.T) {
	t.Parallel()

	t.Run("loads a module that is not loaded, and persists it", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("modprobe", "br_netfilter").Do(func(viaduct.Command) error {
			return os.MkdirAll(env.Path("/sys/module/br_netfilter"), 0o755)
		})

		log, err := env.Apply(env.Context(), LoadModule("br_netfilter"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"modprobe br_netfilter"}, env.Runner.Commands())
		assert.Equal(t, "br_netfilter\n", env.ReadFile("/etc/modules-load.d/br_netfilter.conf"))

		log, err = env.Apply(env.Context(), LoadModule("br_netfilter"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Len(t, env.Runner.Commands(), 1)
	})

	t.Run("a loaded module named with dashes is a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/sys/module/nf_conntrack"), 0o755))

		log, err := env.Apply(env.Context(), &KernelModule{Name: "nf-conntrack"})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
		assert.NoFileExists(t, env.Path("/etc/modules-load.d/nf-conntrack.conf"))
	})

	t.Run("a built-in module without parameters is a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/proc/sys/kernel/osrelease", "6.8.0-generic\n")
		env.WriteFile("/lib/modules/6.8.0-generic/modules.builtin", "kernel/drivers/block/loop.ko\nkernel/net/bridge/br-netfilter.ko\n")

		for _, name := range []string{"loop", "br_netfilter"} {
			log, err := env.Apply(env.Context(), &KernelModule{Name: name})
			require.NoError(t, err)
			assert.False(t, log.Changed(), name)
		}

		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("a dry run changes nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), LoadModule("br_netfilter"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
		assert.NoFileExists(t, env.Path("/etc/modules-load.d/br_netfilter.conf"))
	})

	t.Run("a failed modprobe is an error", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.Runner.On("modprobe", "missing").Exit(1)

		_, err := env.Apply(env.Context(), &KernelModule{Name: "missing"})
		assert.ErrorContains(t, err, "modprobe missing failed")
	})

	t.Run("preflight", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		assert.EqualError(t, (&KernelModule{}).PreflightChecks(env.Context(), env.Log), "required parameter: Name")
		assert.EqualError(t, (&KernelModule{Name: "../x"}).PreflightChecks(env.Context(), env.Log), "invalid module name: ../x")
	})
}
