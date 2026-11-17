package resources

import (
	"os"
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

func TestSwap(t *testing.T) {
	const swaps = "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n"

	setup := func(t *testing.T) *viaducttest.Env {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/proc/swaps", swaps)
		env.WriteFile("/etc/fstab", "LABEL=root / ext4 defaults 0 1\n")
		env.BlockDevice("/dev/sdb")

		return env
	}

	t.Run("preflight", func(t *testing.T) {
		env := setup(t)

		assert.Error(t, (&Swap{}).PreflightChecks(env.Context(), env.Log))
		assert.Error(t, (&Swap{Device: "sdb"}).PreflightChecks(env.Context(), env.Log))
		assert.Error(t, (&Swap{Device: "/dev/sdb", Priority: -1}).PreflightChecks(env.Context(), env.Log))
		assert.NoError(t, (&Swap{Device: "/dev/sdb"}).PreflightChecks(env.Context(), env.Log))
	})

	t.Run("an empty device gets mkswap, an fstab entry and swapon", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Exit(2)
		env.Runner.On("mkswap", "/dev/sdb").Do(func(viaduct.Command) error {
			env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=swap\nUUID=abcd\n")
			return nil
		})
		env.Runner.On("swapon", "-p", "10", "/dev/sdb")

		log, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb", Priority: 10})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{
			"blkid -p -o export /dev/sdb",
			"mkswap /dev/sdb",
			"blkid -p -o export /dev/sdb",
			"swapon -p 10 /dev/sdb",
		}, env.Runner.Commands())
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\nUUID=abcd none swap sw,pri=10 0 0\n", env.ReadFile("/etc/fstab"))
	})

	t.Run("an fstab change reloads systemd", func(t *testing.T) {
		env := setup(t)
		require.NoError(t, os.MkdirAll(env.Path("/run/systemd/system"), 0o755))
		env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=swap\nUUID=abcd\n")
		env.Runner.On("systemctl", "daemon-reload")
		env.Runner.On("swapon", "/dev/sdb")

		_, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		require.NoError(t, err)

		assert.Equal(t, []string{"blkid -p -o export /dev/sdb", "systemctl daemon-reload", "swapon /dev/sdb"}, env.Runner.Commands())

		// Once recorded, nothing is reloaded
		env.WriteFile("/proc/swaps", swaps+"/dev/sdb partition 1048572 0 -2\n")

		log, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		require.NoError(t, err)
		assert.False(t, log.Changed())
		assert.Len(t, env.Runner.Commands(), 4, "only blkid ran again")
	})

	t.Run("existing swap skips mkswap", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=swap\nUUID=abcd\n")
		env.Runner.On("swapon", "/dev/sdb")

		_, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		require.NoError(t, err)

		assert.False(t, env.Runner.Ran("mkswap", "/dev/sdb"))
		assert.True(t, env.Runner.Ran("swapon", "/dev/sdb"))
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\nUUID=abcd none swap sw 0 0\n", env.ReadFile("/etc/fstab"))
	})

	t.Run("an active swap with another priority is a warning", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=swap\nUUID=abcd\n")
		env.WriteFile("/etc/fstab", "UUID=abcd none swap sw,pri=10 0 0\n")
		env.WriteFile("/proc/swaps", swaps+"/dev/sdb partition 1048572 0 5\n")

		log, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb", Priority: 10})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"priority-differs"}, env.Recorder.Messages(viaduct.LevelWarn))
		assert.False(t, env.Runner.Ran("swapon", "-p", "10", "/dev/sdb"))
	})

	t.Run("active and recorded is a noop, compared by realpath", func(t *testing.T) {
		env := setup(t)
		env.WriteFile("/dev/nvme1n1", "")
		require.NoError(t, os.Remove(env.Path("/dev/sdb")))
		require.NoError(t, os.Symlink("nvme1n1", env.Path("/dev/sdb")))
		env.WriteFile("/proc/swaps", swaps+"/dev/nvme1n1                            partition\t1048572\t\t0\t\t-2\n")
		env.WriteFile("/etc/fstab", "UUID=abcd none swap sw 0 0\n")
		env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=swap\nUUID=abcd\n")

		log, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"blkid -p -o export /dev/sdb"}, env.Runner.Commands())
	})

	t.Run("an active swap missing from fstab is recorded", func(t *testing.T) {
		env := setup(t)
		env.WriteFile("/proc/swaps", swaps+"/dev/sdb partition 1048572 0 -2\n")
		env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=swap\nUUID=abcd\n")

		log, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Contains(t, env.ReadFile("/etc/fstab"), "UUID=abcd none swap sw 0 0\n")
		assert.False(t, env.Runner.Ran("swapon", "/dev/sdb"))
	})

	t.Run("never touches a device with a filesystem", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=ext4\nUUID=abcd\n")

		_, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		require.Error(t, err)

		assert.True(t, viaduct.IsPermanent(err))
		assert.ErrorContains(t, err, "wipefs -a")
		assert.Len(t, env.Runner.Calls(), 1)
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n", env.ReadFile("/etc/fstab"))
	})

	t.Run("never touches a device with a partition table", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Stdout("PTTYPE=gpt\nPTUUID=abcd\n")

		_, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		require.ErrorContains(t, err, "a partition table of type gpt")

		assert.True(t, viaduct.IsPermanent(err))
		assert.Len(t, env.Runner.Calls(), 1)
	})

	t.Run("a file that is not a block device is refused", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/proc/swaps", swaps)
		env.WriteFile("/dev/sdb", "")

		_, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		require.ErrorContains(t, err, "is not a block device")
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("a failed mkswap is an error and records nothing", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Exit(2)
		env.Runner.On("mkswap", "/dev/sdb").Exit(1)

		_, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		require.ErrorContains(t, err, "mkswap failed on /dev/sdb")
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n", env.ReadFile("/etc/fstab"))
	})

	t.Run("no UUID after mkswap is an error and records nothing", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Exit(2)
		env.Runner.On("mkswap", "/dev/sdb").Do(func(viaduct.Command) error {
			env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=swap\n")
			return nil
		})

		_, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		require.ErrorContains(t, err, "no UUID")
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n", env.ReadFile("/etc/fstab"))
	})

	t.Run("a failed swapon is an error", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Stdout("TYPE=swap\nUUID=abcd\n")
		env.Runner.On("swapon", "/dev/sdb").Exit(255)

		_, err := env.Apply(env.Context(), &Swap{Device: "/dev/sdb"})
		assert.ErrorContains(t, err, "swapon failed on /dev/sdb")
	})

	t.Run("dry run", func(t *testing.T) {
		env := setup(t)
		env.Runner.On(blkid, blkidArgs...).Exit(2)

		log, err := env.Apply(env.DryRunContext(), &Swap{Device: "/dev/sdb"})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"blkid -p -o export /dev/sdb"}, env.Runner.Commands())
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n", env.ReadFile("/etc/fstab"))
	})
}
