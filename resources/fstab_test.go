package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

func TestFstabEscape(t *testing.T) {
	assert.Equal(t, `/mnt/with\040space`, fstabEscape("/mnt/with space"))
	assert.Equal(t, "/mnt/with space", fstabUnescape(`/mnt/with\040space`))
	assert.Equal(t, `a\134b`, fstabEscape(`a\b`))
	assert.Equal(t, `a\b`, fstabUnescape(`a\134b`))
	assert.Equal(t, `trailing\04`, fstabUnescape(`trailing\04`), "an incomplete escape is left alone")
}

func TestEnsureFstabEntry(t *testing.T) {
	entry := fstabEntry{Source: "/dev/sdb", Path: "/data", FSType: "ext4", Options: "defaults,nofail", Pass: 2}
	byPath := func(fields []string) bool { return len(fields) > 1 && fields[1] == "/data" }

	t.Run("creates the file", func(t *testing.T) {
		env := viaducttest.NewEnv(t)

		changed, err := ensureFstabEntry(env.Context(), env.Log, entry, byPath)
		require.NoError(t, err)

		assert.True(t, changed)
		assert.Equal(t, "/dev/sdb /data ext4 defaults,nofail 0 2\n", env.ReadFile("/etc/fstab"))
	})

	t.Run("appends, keeping everything else", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/etc/fstab", "# comment\nLABEL=root / ext4 defaults 0 1\n\n")

		changed, err := ensureFstabEntry(env.Context(), env.Log, entry, byPath)
		require.NoError(t, err)

		assert.True(t, changed)
		assert.Equal(t, "# comment\nLABEL=root / ext4 defaults 0 1\n\n/dev/sdb /data ext4 defaults,nofail 0 2\n", env.ReadFile("/etc/fstab"))
	})

	t.Run("replaces the first match in place and removes the rest", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/etc/fstab", "LABEL=root / ext4 defaults 0 1\n/dev/old /data xfs defaults 0 0\ntmpfs /tmp tmpfs defaults 0 0\n/dev/older /data ext4 defaults\n")

		changed, err := ensureFstabEntry(env.Context(), env.Log, entry, byPath)
		require.NoError(t, err)

		assert.True(t, changed)
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n/dev/sdb /data ext4 defaults,nofail 0 2\ntmpfs /tmp tmpfs defaults 0 0\n", env.ReadFile("/etc/fstab"))
	})

	t.Run("an equivalent entry is a noop, even with different spacing", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		original := "/dev/sdb\t/data   ext4 defaults,nofail 0 2\n"
		env.WriteFile("/etc/fstab", original)

		before, err := os.Stat(env.Path("/etc/fstab"))
		require.NoError(t, err)

		changed, err := ensureFstabEntry(env.Context(), env.Log, entry, byPath)
		require.NoError(t, err)

		assert.False(t, changed)
		assert.Equal(t, original, env.ReadFile("/etc/fstab"))

		after, err := os.Stat(env.Path("/etc/fstab"))
		require.NoError(t, err)
		assert.True(t, os.SameFile(before, after), "an unchanged file is not rewritten")
	})

	t.Run("missing dump and pass count as zero", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/etc/fstab", "tmpfs /data tmpfs defaults\n")

		changed, err := ensureFstabEntry(env.Context(), env.Log, fstabEntry{Source: "tmpfs", Path: "/data", FSType: "tmpfs", Options: "defaults"}, byPath)
		require.NoError(t, err)
		assert.False(t, changed)
	})

	t.Run("dry run reports the change and writes nothing", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/etc/fstab", "LABEL=root / ext4 defaults 0 1\n")

		changed, err := ensureFstabEntry(env.DryRunContext(), env.Log, entry, byPath)
		require.NoError(t, err)

		assert.True(t, changed)
		assert.True(t, env.Log.Changed())
		assert.Equal(t, "LABEL=root / ext4 defaults 0 1\n", env.ReadFile("/etc/fstab"))
	})

	t.Run("paths with spaces are escaped", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		spaced := fstabEntry{Source: "/dev/sdb", Path: "/mnt/my data", FSType: "ext4", Options: "defaults"}

		_, err := ensureFstabEntry(env.Context(), env.Log, spaced, func(fields []string) bool { return fields[1] == "/mnt/my data" })
		require.NoError(t, err)
		assert.Equal(t, "/dev/sdb /mnt/my\\040data ext4 defaults 0 0\n", env.ReadFile("/etc/fstab"))

		changed, err := ensureFstabEntry(env.Context(), env.Log, spaced, func(fields []string) bool { return fields[1] == "/mnt/my data" })
		require.NoError(t, err)
		assert.False(t, changed, "the escaped entry is recognised on the next run")
	})
}

func TestMountInfo(t *testing.T) {
	env := viaducttest.NewEnv(t)
	env.WriteFile("/proc/self/mountinfo", `22 1 259:1 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p1 rw
30 22 259:2 / /data rw,relatime shared:2 - xfs /dev/nvme1n1 rw,attr2
31 22 0:40 / /mnt/my\040data rw - tmpfs tmpfs rw
32 30 0:41 / /data rw - tmpfs tmpfs rw
`)

	e := viaduct.EnvFrom(env.Context())

	m, ok, err := mountedAt(e, "/mnt/my data")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "tmpfs", m.Source)

	m, ok, err = mountedAt(e, "/data/")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "tmpfs", m.Source, "the last mount stacked on a path is the visible one")

	_, ok, err = mountedAt(e, "/missing")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestDevicePath(t *testing.T) {
	env := viaducttest.NewEnv(t)
	e := viaduct.EnvFrom(env.Context())

	env.WriteFile("/dev/nvme1n1", "")
	require.NoError(t, os.MkdirAll(env.Path("/dev/disk/by-uuid"), 0o755))
	require.NoError(t, os.Symlink("../../nvme1n1", env.Path("/dev/disk/by-uuid/1234")))
	require.NoError(t, os.Symlink("nvme1n1", env.Path("/dev/sdb")))

	assert.Equal(t, "/dev/nvme1n1", devicePath(e, "UUID=1234"))
	assert.Equal(t, "/dev/nvme1n1", devicePath(e, `UUID="1234"`))
	assert.Equal(t, "/dev/nvme1n1", devicePath(e, "/dev/sdb"))
	assert.Equal(t, "/dev/nvme1n1", devicePath(e, "/dev/nvme1n1"))
	assert.Equal(t, "tmpfs", devicePath(e, "tmpfs"))
	assert.Equal(t, "/dev/disk/by-uuid/missing", devicePath(e, "UUID=missing"))

	assert.True(t, sameDevice(e, "UUID=1234", "/dev/sdb"))
	assert.False(t, sameDevice(e, "UUID=1234", "tmpfs"))
}

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

func TestReloadSystemd(t *testing.T) {
	t.Run("without systemd", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		require.NoError(t, reloadSystemd(env.Context(), env.Log))
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("with systemd", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(filepath.Join(env.Root, "run/systemd/system"), 0o755))
		env.Runner.On("systemctl", "daemon-reload")

		require.NoError(t, reloadSystemd(env.Context(), env.Log))
		assert.True(t, env.Runner.Ran("systemctl", "daemon-reload"))
	})
}
