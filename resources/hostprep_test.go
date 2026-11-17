package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

const blkid = "blkid"

var blkidArgs = []string{"-p", "-o", "export", "/dev/sdb"}

func TestFilesystem(t *testing.T) {
	t.Run("takes FstabLock", func(t *testing.T) {
		assert.Equal(t, viaduct.FstabLock, Mkfs("/dev/sdb", "ext4").Params().LockKey)
	})

	setup := func(t *testing.T) *viaducttest.Env {
		env := viaducttest.NewEnv(t)
		env.BlockDevice("/dev/sdb")
		return env
	}

	t.Run("preflight", func(t *testing.T) {
		env := setup(t)

		for _, f := range []*Filesystem{
			{Type: "ext4"},
			{Device: "/dev/sdb"},
			{Device: "sdb", Type: "ext4"},
			{Device: "/dev/sdb", Type: "ext4; rm -rf /"},
		} {
			assert.Error(t, f.PreflightChecks(env.Context(), env.Log), "%+v", f)
		}

		assert.NoError(t, Mkfs("/dev/sdb", "ext4").PreflightChecks(env.Context(), env.Log))
	})

	t.Run("formats an empty device", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Exit(2)
		env.Runner.On("mkfs", "-t", "ext4", "-L", "data", "/dev/sdb")

		log, err := env.Apply(env.Context(), &Filesystem{Device: "/dev/sdb", Type: "ext4", Options: []string{"-L", "data"}})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.True(t, env.Runner.Ran("mkfs", "-t", "ext4", "-L", "data", "/dev/sdb"))
	})

	t.Run("the same filesystem is a noop", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=ext4\nUUID=abcd\n")

		log, err := env.Apply(env.Context(), Mkfs("/dev/sdb", "ext4"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"blkid -p -o export /dev/sdb"}, env.Runner.Commands())
	})

	for name, out := range map[string]string{
		"another filesystem":                 "TYPE=xfs\nUUID=abcd\n",
		"a partition table":                  "PTTYPE=gpt\nPTUUID=abcd\n",
		"a filesystem and a partition table": "TYPE=ext4\nPTTYPE=dos\n",
		"an unrecognised signature":          "USAGE=other\n",
	} {
		t.Run("never formats a device with "+name, func(t *testing.T) {
			env := setup(t)
			env.Runner.On(blkid, blkidArgs...).Stdout(out)

			_, err := env.Apply(env.Context(), Mkfs("/dev/sdb", "ext4"))
			require.Error(t, err)

			assert.True(t, viaduct.IsPermanent(err))
			assert.Len(t, env.Runner.Calls(), 1, "only blkid runs")
		})
	}

	t.Run("a blkid failure is permanent and formats nothing", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Exit(4)

		_, err := env.Apply(env.Context(), Mkfs("/dev/sdb", "ext4"))
		assert.True(t, viaduct.IsPermanent(err))
		assert.Len(t, env.Runner.Calls(), 1)
	})

	t.Run("a missing device is an error worth retrying", func(t *testing.T) {
		env := viaducttest.NewEnv(t)

		_, err := env.Apply(env.Context(), Mkfs("/dev/sdb", "ext4"))
		require.Error(t, err)
		assert.False(t, viaduct.IsPermanent(err))
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("msdos and fat compare as vfat", func(t *testing.T) {
		for _, fstype := range []string{"msdos", "fat", "vfat"} {
			env := setup(t)
			env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=vfat\nUUID=ABCD-1234\n")

			log, err := env.Apply(env.Context(), Mkfs("/dev/sdb", fstype))
			require.NoError(t, err, fstype)
			assert.False(t, log.Changed(), fstype)
		}
	})

	t.Run("the refusal says how to proceed", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=xfs\n")

		_, err := env.Apply(env.Context(), Mkfs("/dev/sdb", "ext4"))
		assert.ErrorContains(t, err, "wipefs -a")
	})

	t.Run("a file that is not a block device is refused", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/dev/sdb", "")

		_, err := env.Apply(env.Context(), Mkfs("/dev/sdb", "ext4"))
		require.ErrorContains(t, err, "is not a block device")
		assert.True(t, viaduct.IsPermanent(err))
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("a failed mkfs is an error", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Exit(2)
		env.Runner.On("mkfs", "-t", "ext4", "/dev/sdb").Exit(1)

		_, err := env.Apply(env.Context(), Mkfs("/dev/sdb", "ext4"))
		assert.ErrorContains(t, err, "mkfs failed on /dev/sdb")
	})

	t.Run("dry run probes but does not format", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Exit(2)

		log, err := env.Apply(env.DryRunContext(), Mkfs("/dev/sdb", "ext4"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"blkid -p -o export /dev/sdb"}, env.Runner.Commands())
	})
}
