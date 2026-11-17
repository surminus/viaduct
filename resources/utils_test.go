package resources

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct/viaducttest"
)

func TestPermissionsDoNotFollowSymlinks(t *testing.T) {
	t.Run("chmod and chown refuse a symlink", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		outside := filepath.Join(t.TempDir(), "secret")
		require.NoError(t, os.WriteFile(outside, nil, 0o600))
		require.NoError(t, os.MkdirAll(env.Path("/etc"), 0o755))
		require.NoError(t, os.Symlink(outside, env.Path("/etc/app.conf")))

		assert.ErrorContains(t, applyChmod(env.Context(), env.Log, env.Path("/etc/app.conf"), 0o644), "is a symlink")
		assert.ErrorContains(t, applyChown(env.Context(), env.Log, env.Path("/etc/app.conf"), os.Getuid(), os.Getgid()), "is a symlink")

		info, err := os.Stat(outside)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the target is untouched")
	})

	t.Run("permissions only on a symlink is an error", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		outside := filepath.Join(t.TempDir(), "secret")
		require.NoError(t, os.WriteFile(outside, nil, 0o600))
		require.NoError(t, os.MkdirAll(env.Path("/etc"), 0o755))
		require.NoError(t, os.Symlink(outside, env.Path("/etc/app.conf")))

		_, err := env.Apply(env.Context(), SetPermissions("/etc/app.conf", 0o644))
		assert.ErrorContains(t, err, "is a symlink")
	})

	t.Run("a managed file behind a safe symlink is written and set at its target", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/etc/real.conf", "old\n")
		require.NoError(t, os.Symlink("real.conf", env.Path("/etc/app.conf")))

		f := &File{Path: "/etc/app.conf", Content: "new\n", Permissions: Permissions{Mode: 0o600, UID: os.Getuid(), GID: os.Getgid()}}
		_, err := env.Apply(env.Context(), f)
		require.NoError(t, err)

		assert.Equal(t, "new\n", env.ReadFile("/etc/real.conf"))

		info, err := os.Lstat(env.Path("/etc/app.conf"))
		require.NoError(t, err)
		assert.NotZero(t, info.Mode()&fs.ModeSymlink, "the link is kept")

		info, err = os.Stat(env.Path("/etc/real.conf"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("a directory that is a symlink is an error", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		outside := t.TempDir()
		require.NoError(t, os.Symlink(outside, env.Path("/srv")))

		_, err := env.Apply(env.Context(), &Directory{Path: "/srv", Permissions: Permissions{UID: os.Getuid(), GID: os.Getgid()}})
		assert.ErrorContains(t, err, "is a symlink")
	})
}

// Not parallel: these replace the package's lstat and lchown.
func TestChownTree(t *testing.T) {
	record := func(t *testing.T) *[]string {
		t.Helper()

		var calls []string

		orig := lchown
		lchown = func(p string, uid, gid int) error {
			calls = append(calls, p)
			return orig(p, uid, gid)
		}
		t.Cleanup(func() { lchown = orig })

		return &calls
	}

	t.Run("symlinks are changed themselves and never followed", func(t *testing.T) {
		calls := record(t)

		outside := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(outside, "file"), nil, 0o644))

		tree := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tree, "inside"), nil, 0o644))
		require.NoError(t, os.Symlink(filepath.Join(outside, "file"), filepath.Join(tree, "file-link")))
		require.NoError(t, os.Symlink(outside, filepath.Join(tree, "dir-link")))

		// -1 leaves ownership alone, so this runs unprivileged, and never
		// matches, so every path is changed
		changed, err := chownTree(tree, -1, -1, false)
		require.NoError(t, err)
		assert.True(t, changed)

		slices.Sort(*calls)
		assert.Equal(t, []string{
			tree,
			filepath.Join(tree, "dir-link"),
			filepath.Join(tree, "file-link"),
			filepath.Join(tree, "inside"),
		}, *calls)
	})

	t.Run("a file that vanishes part way through is skipped", func(t *testing.T) {
		tree := t.TempDir()
		for _, name := range []string{"a", "b", "c"} {
			require.NoError(t, os.WriteFile(filepath.Join(tree, name), nil, 0o644))
		}

		origStat, origChown := lstat, lchown
		t.Cleanup(func() { lstat, lchown = origStat, origChown })

		lstat = func(p string) (fs.FileInfo, error) {
			if filepath.Base(p) == "a" {
				return nil, fs.ErrNotExist
			}

			return origStat(p)
		}

		var calls []string
		lchown = func(p string, uid, gid int) error {
			if filepath.Base(p) == "b" {
				return &fs.PathError{Op: "lchown", Path: p, Err: fs.ErrNotExist}
			}

			calls = append(calls, p)

			return origChown(p, uid, gid)
		}

		changed, err := chownTree(tree, -1, -1, false)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, []string{tree, filepath.Join(tree, "c")}, calls)
	})

	t.Run("a dry run changes nothing", func(t *testing.T) {
		calls := record(t)
		tree := t.TempDir()

		changed, err := chownTree(tree, -1, -1, true)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Empty(t, *calls)
	})
}
