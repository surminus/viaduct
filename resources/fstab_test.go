package resources

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

func TestProbeDevice(t *testing.T) {
	setup := func(t *testing.T) *viaducttest.Env {
		env := viaducttest.NewEnv(t)
		env.BlockDevice("/dev/sdb")
		return env
	}

	t.Run("empty", func(t *testing.T) {
		env := setup(t)
		env.Runner.On("blkid", "-p", "-o", "export", "/dev/sdb").Exit(2)

		p, err := probeDevice(env.Context(), env.Log, "/dev/sdb")
		require.NoError(t, err)
		assert.True(t, p.Empty)
	})

	t.Run("filesystem", func(t *testing.T) {
		env := setup(t)
		env.Runner.On("blkid", "-p", "-o", "export", "/dev/sdb").Stdout("DEVNAME=/dev/sdb\nUUID=abcd\nTYPE=ext4\nUSAGE=filesystem\n")

		p, err := probeDevice(env.Context(), env.Log, "/dev/sdb")
		require.NoError(t, err)
		assert.Equal(t, probe{Type: "ext4", UUID: "abcd"}, p)
	})

	t.Run("partition table", func(t *testing.T) {
		env := setup(t)
		env.Runner.On("blkid", "-p", "-o", "export", "/dev/sdb").Stdout("DEVNAME=/dev/sdb\nPTUUID=abcd\nPTTYPE=gpt\n")

		p, err := probeDevice(env.Context(), env.Log, "/dev/sdb")
		require.NoError(t, err)
		assert.False(t, p.Empty)
		assert.Equal(t, "gpt", p.PartitionTable)
		assert.Equal(t, "a partition table of type gpt", p.describe())
	})

	t.Run("a missing device is an error rather than empty", func(t *testing.T) {
		env := viaducttest.NewEnv(t)

		_, err := probeDevice(env.Context(), env.Log, "/dev/sdb")
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.False(t, viaduct.IsPermanent(err), "the device may appear, so it is worth trying again")
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("any other blkid failure is permanent", func(t *testing.T) {
		env := setup(t)
		env.Runner.On("blkid", "-p", "-o", "export", "/dev/sdb").Exit(8)

		_, err := probeDevice(env.Context(), env.Log, "/dev/sdb")
		assert.True(t, viaduct.IsPermanent(err))
	})

	t.Run("runs in a dry run", func(t *testing.T) {
		env := setup(t)
		env.Runner.On("blkid", "-p", "-o", "export", "/dev/sdb").Exit(2)

		_, err := probeDevice(env.DryRunContext(), env.Log, "/dev/sdb")
		require.NoError(t, err)
		assert.True(t, env.Runner.Ran("blkid", "-p", "-o", "export", "/dev/sdb"))
	})
}
