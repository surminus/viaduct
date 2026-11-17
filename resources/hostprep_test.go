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

func TestMount(t *testing.T) {
	const mountinfo = "22 1 259:1 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p1 rw\n"

	setup := func(t *testing.T) *viaducttest.Env {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/proc/self/mountinfo", mountinfo)
		env.WriteFile("/etc/fstab", "LABEL=root / ext4 defaults 0 1\n")
		env.WriteFile("/dev/nvme1n1", "")
		require.NoError(t, os.Symlink("nvme1n1", env.Path("/dev/sdb")))
		require.NoError(t, os.MkdirAll(env.Path("/run/systemd/system"), 0o755))

		return env
	}

	mountArgs := []string{"-t", "ext4", "-o", "defaults,nofail", "/dev/sdb", "/data"}

	mount := func() *Mount {
		return &Mount{Source: "/dev/sdb", Path: "/data", FSType: "ext4", Options: "defaults,nofail", Pass: 2}
	}

	t.Run("preflight", func(t *testing.T) {
		env := setup(t)

		for _, m := range []*Mount{
			{Path: "/data", FSType: "ext4"},
			{Source: "/dev/sdb", FSType: "ext4"},
			{Source: "/dev/sdb", Path: "/", FSType: "ext4"},
			{Source: "/dev/sdb", Path: "data", FSType: "ext4"},
			{Source: "/dev/sdb", Path: "/data"},
		} {
			assert.Error(t, m.PreflightChecks(env.Context(), env.Log), "%+v", m)
		}

		m := &Mount{Source: "/dev/sdb", Path: "/data", FSType: "ext4"}
		require.NoError(t, m.PreflightChecks(env.Context(), env.Log))
		assert.Equal(t, "defaults", m.Options)
		assert.Equal(t, os.FileMode(0o755), m.Mode)
		assert.Equal(t, viaduct.FstabLock, m.Params().LockKey)
	})

	t.Run("mounts, then records the mount", func(t *testing.T) {
		env := setup(t)
		env.Runner.On("systemctl", "daemon-reload")
		env.Runner.On("mount", mountArgs...)

		log, err := env.Apply(env.Context(), mount())
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.DirExists(t, env.Path("/data"))
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n/dev/sdb /data ext4 defaults,nofail 0 2\n", env.ReadFile("/etc/fstab"))
		assert.Equal(t, []string{"mount -t ext4 -o defaults,nofail /dev/sdb /data", "systemctl daemon-reload"}, env.Runner.Commands())
	})

	t.Run("a failed mount leaves fstab alone", func(t *testing.T) {
		env := setup(t)
		env.Runner.On("mount", mountArgs...).Exit(32)

		_, err := env.Apply(env.Context(), mount())
		require.Error(t, err)

		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n", env.ReadFile("/etc/fstab"))
		assert.Equal(t, []string{"mount -t ext4 -o defaults,nofail /dev/sdb /data"}, env.Runner.Commands())
	})

	t.Run("already mounted under a symlinked parent is seen as mounted", func(t *testing.T) {
		env := setup(t)
		require.NoError(t, os.MkdirAll(env.Path("/srv/data"), 0o755))
		require.NoError(t, os.Symlink("srv", env.Path("/mnt")))
		env.WriteFile("/proc/self/mountinfo", mountinfo+"30 22 259:2 / /srv/data rw - ext4 /dev/nvme1n1 rw\n")
		env.WriteFile("/etc/fstab", "/dev/sdb /mnt/data ext4 defaults 0 0\n")

		log, err := env.Apply(env.Context(), &Mount{Source: "/dev/sdb", Path: "/mnt/data", FSType: "ext4"})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("a mount point that is a symlink is refused", func(t *testing.T) {
		env := setup(t)
		require.NoError(t, os.MkdirAll(env.Path("/elsewhere"), 0o755))
		require.NoError(t, os.Symlink("elsewhere", env.Path("/data")))

		_, err := env.Apply(env.Context(), mount())
		require.ErrorContains(t, err, "is a symlink")

		assert.True(t, viaduct.IsPermanent(err))
		assert.Empty(t, env.Runner.Calls())
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n", env.ReadFile("/etc/fstab"))
	})

	t.Run("already mounted and recorded is a noop", func(t *testing.T) {
		env := setup(t)
		env.WriteFile("/proc/self/mountinfo", mountinfo+"30 22 259:2 / /data rw - ext4 /dev/nvme1n1 rw\n")
		env.WriteFile("/etc/fstab", "/dev/sdb /data ext4 defaults,nofail 0 2\n")
		require.NoError(t, os.Mkdir(env.Path("/data"), 0o755))

		log, err := env.Apply(env.Context(), mount())
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("mounted by UUID", func(t *testing.T) {
		env := setup(t)
		require.NoError(t, os.MkdirAll(env.Path("/dev/disk/by-uuid"), 0o755))
		require.NoError(t, os.Symlink("../../nvme1n1", env.Path("/dev/disk/by-uuid/1234")))
		env.WriteFile("/proc/self/mountinfo", mountinfo+"30 22 259:2 / /data rw - ext4 /dev/nvme1n1 rw\n")
		env.WriteFile("/etc/fstab", "UUID=1234 /data ext4 defaults 0 0\n")
		require.NoError(t, os.Mkdir(env.Path("/data"), 0o755))

		log, err := env.Apply(env.Context(), &Mount{Source: "UUID=1234", Path: "/data", FSType: "ext4"})
		require.NoError(t, err)
		assert.False(t, log.Changed(), "the mounted device is the one the UUID names")
	})

	t.Run("an fstab change while mounted reloads systemd but does not remount", func(t *testing.T) {
		env := setup(t)
		env.WriteFile("/proc/self/mountinfo", mountinfo+"30 22 259:2 / /data rw - ext4 /dev/nvme1n1 rw\n")
		require.NoError(t, os.Mkdir(env.Path("/data"), 0o755))
		env.Runner.On("systemctl", "daemon-reload")

		log, err := env.Apply(env.Context(), mount())
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"systemctl daemon-reload"}, env.Runner.Commands())
	})

	t.Run("mounted from somewhere else is an error and changes nothing", func(t *testing.T) {
		env := setup(t)
		env.WriteFile("/dev/nvme2n1", "")
		env.WriteFile("/proc/self/mountinfo", mountinfo+"30 22 259:3 / /data rw - ext4 /dev/nvme2n1 rw\n")

		_, err := env.Apply(env.Context(), mount())
		require.Error(t, err)

		assert.True(t, viaduct.IsPermanent(err))
		assert.ErrorContains(t, err, "already mounted from /dev/nvme2n1")
		assert.ErrorContains(t, err, "unmount it or change Source")
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n", env.ReadFile("/etc/fstab"))
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("part of the same device mounted there is an error", func(t *testing.T) {
		env := setup(t)
		env.WriteFile("/proc/self/mountinfo", mountinfo+"30 22 259:2 /subdir /data rw - ext4 /dev/nvme1n1 rw\n")

		_, err := env.Apply(env.Context(), mount())
		require.Error(t, err)

		assert.ErrorContains(t, err, "already mounted from /dev/nvme1n1[/subdir]")
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("a btrfs subvolume compares by its root", func(t *testing.T) {
		env := setup(t)
		env.WriteFile("/proc/self/mountinfo", mountinfo+"30 22 0:40 /@data /data rw - btrfs /dev/nvme1n1 rw\n")

		m := func(options string) *Mount {
			return &Mount{Source: "/dev/sdb", Path: "/data", FSType: "btrfs", Options: options}
		}

		env.Runner.On("systemctl", "daemon-reload")

		_, err := env.Apply(env.Context(), m("subvol=@data"))
		require.NoError(t, err)

		_, err = env.Apply(env.Context(), m("subvolid=256"))
		require.NoError(t, err)

		_, err = env.Apply(env.Context(), m("subvol=@other"))
		assert.ErrorContains(t, err, "already mounted from /dev/nvme1n1[/@data]")
	})

	t.Run("a file at the mount point is an error", func(t *testing.T) {
		env := setup(t)
		env.WriteFile("/data", "")

		_, err := env.Apply(env.Context(), mount())
		assert.ErrorContains(t, err, "is not a directory")
	})

	t.Run("bind mounts compare by the directory they come from", func(t *testing.T) {
		// /srv is on the root filesystem, so binding /srv/a shows that
		// device with a root of /srv/a
		bound := mountinfo + "30 22 259:1 /srv/a /data rw - ext4 /dev/nvme0n1p1 rw\n"
		bind := func(source string) *Mount {
			return &Mount{Source: source, Path: "/data", FSType: "none", Options: "bind"}
		}

		env := setup(t)
		require.NoError(t, os.MkdirAll(env.Path("/srv/a"), 0o755))
		require.NoError(t, os.MkdirAll(env.Path("/srv/b"), 0o755))
		require.NoError(t, os.Mkdir(env.Path("/data"), 0o755))
		env.WriteFile("/proc/self/mountinfo", bound)
		env.WriteFile("/etc/fstab", "/srv/a /data none bind 0 0\n")

		log, err := env.Apply(env.Context(), bind("/srv/a"))
		require.NoError(t, err)
		assert.False(t, log.Changed())

		_, err = env.Apply(env.Context(), bind("/srv/b"))
		assert.ErrorContains(t, err, "already mounted")

		env.WriteFile("/proc/self/mountinfo", mountinfo)
		env.Runner.On("mount", "-t", "none", "-o", "bind", "/srv/b", "/data")
		env.Runner.On("systemctl", "daemon-reload")

		_, err = env.Apply(env.Context(), bind("/srv/b"))
		require.NoError(t, err)
		assert.True(t, env.Runner.Ran("mount", "-t", "none", "-o", "bind", "/srv/b", "/data"))
	})

	t.Run("dry run", func(t *testing.T) {
		env := setup(t)

		log, err := env.Apply(env.DryRunContext(), mount())
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"created-mount-point", "mounted", "fstab-updated", "daemon-reload"}, env.Recorder.Messages(viaduct.LevelChanged))
		assert.NoDirExists(t, env.Path("/data"))
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n", env.ReadFile("/etc/fstab"))
		assert.Empty(t, env.Runner.Calls())
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
