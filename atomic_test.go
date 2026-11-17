package viaduct

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomic(t *testing.T) {
	read := func(t *testing.T, path string) string {
		t.Helper()

		b, err := os.ReadFile(path)
		require.NoError(t, err)

		return string(b)
	}

	// leftovers returns anything in dir other than the given names
	leftovers := func(t *testing.T, dir string, names ...string) []string {
		t.Helper()

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)

		var out []string
		for _, e := range entries {
			if !contains(names, e.Name()) {
				out = append(out, e.Name())
			}
		}

		return out
	}

	t.Run("creates a file with the given mode", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "new.conf")

		require.NoError(t, WriteFileAtomic(path, []byte("hello\n"), 0o640))

		assert.Equal(t, "hello\n", read(t, path))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
		assert.Empty(t, leftovers(t, dir, "new.conf"))
	})

	t.Run("replaces a file, keeping its mode", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "fstab")
		require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o600))

		before, err := os.Stat(path)
		require.NoError(t, err)

		require.NoError(t, WriteFileAtomic(path, []byte("new\n"), 0o644))

		assert.Equal(t, "new\n", read(t, path))

		after, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), after.Mode().Perm())
		assert.False(t, os.SameFile(before, after), "the file is replaced, not rewritten")
		assert.Empty(t, leftovers(t, dir, "fstab"))
	})

	t.Run("keeps setuid, setgid and sticky bits", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "tool")
		require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o755))

		special := os.ModeSetuid | os.ModeSetgid | os.ModeSticky
		require.NoError(t, os.Chmod(path, 0o755|special))

		require.NoError(t, WriteFileAtomic(path, []byte("new\n"), 0o644))

		after, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, 0o755|special, after.Mode()&(os.ModePerm|special))
	})

	t.Run("follows a symlink", func(t *testing.T) {
		dir := t.TempDir()
		real := filepath.Join(dir, "real")
		link := filepath.Join(dir, "link")

		require.NoError(t, os.WriteFile(real, []byte("old\n"), 0o644))
		require.NoError(t, os.Symlink("real", link))

		require.NoError(t, WriteFileAtomic(link, []byte("new\n"), 0o644))

		assert.Equal(t, "new\n", read(t, real))

		info, err := os.Lstat(link)
		require.NoError(t, err)
		assert.Equal(t, os.ModeSymlink, info.Mode()&os.ModeSymlink, "the link is still a link")
	})

	t.Run("follows a dangling symlink", func(t *testing.T) {
		dir := t.TempDir()
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink("missing", link))

		require.NoError(t, WriteFileAtomic(link, []byte("new\n"), 0o644))

		assert.Equal(t, "new\n", read(t, filepath.Join(dir, "missing")))
	})

	t.Run("a failure leaves the original alone", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can write to a read-only directory")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "file")
		require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o444))
		require.NoError(t, os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		assert.Error(t, WriteFileAtomic(path, []byte("new\n"), 0o644))
		assert.Equal(t, "old\n", read(t, path))
	})

	t.Run("a writable file in a read-only directory is written in place", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can write to a read-only directory")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "file")
		require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o644))
		require.NoError(t, os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		require.NoError(t, WriteFileAtomic(path, []byte("new\n"), 0o644))
		assert.Equal(t, "new\n", read(t, path))
	})

	t.Run("a new file in a read-only directory is an error", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can write to a read-only directory")
		}

		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		assert.Error(t, WriteFileAtomic(filepath.Join(dir, "new"), []byte("new\n"), 0o644))
	})

	t.Run("the temporary file is hidden and keeps no suffix", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "99-test.conf")

		// Leave the temporary file behind by making the rename fail
		require.NoError(t, os.Mkdir(path, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(path, "x"), nil, 0o644))

		assert.Error(t, WriteFileAtomic(path, []byte("new\n"), 0o644))
		assert.Empty(t, leftovers(t, dir, "99-test.conf"), "the temporary file is removed on failure")
	})

	t.Run("follows a chain of symlinks", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "real"), []byte("old\n"), 0o644))
		require.NoError(t, os.Symlink("real", filepath.Join(dir, "middle")))
		require.NoError(t, os.Symlink(filepath.Join(dir, "middle"), filepath.Join(dir, "link")))

		require.NoError(t, WriteFileAtomic(filepath.Join(dir, "link"), []byte("new\n"), 0o644))

		assert.Equal(t, "new\n", read(t, filepath.Join(dir, "real")))
		assert.Empty(t, leftovers(t, dir, "real", "middle", "link"))
	})

	t.Run("a relative link under a symlinked directory resolves as the kernel does", func(t *testing.T) {
		dir := t.TempDir()

		// var/run -> ../run, and var/run/x -> ../y, which the kernel reads
		// from run, so it names y at the top, not var/y
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "var"), 0o755))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "run"), 0o755))
		require.NoError(t, os.Symlink("../run", filepath.Join(dir, "var", "run")))
		require.NoError(t, os.Symlink("../y", filepath.Join(dir, "run", "x")))

		require.NoError(t, WriteFileAtomic(filepath.Join(dir, "var", "run", "x"), []byte("new\n"), 0o644))

		assert.Equal(t, "new\n", read(t, filepath.Join(dir, "y")))
		assert.NoFileExists(t, filepath.Join(dir, "var", "y"))
	})

	t.Run("a symlink loop is an error", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Symlink("b", filepath.Join(dir, "a")))
		require.NoError(t, os.Symlink("a", filepath.Join(dir, "b")))

		assert.ErrorIs(t, WriteFileAtomic(filepath.Join(dir, "a"), []byte("new\n"), 0o644), syscall.ELOOP)
	})

	t.Run("does not follow a symlink owned by another user", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("needs root to own a link as another user")
		}

		dir := t.TempDir()
		real := filepath.Join(dir, "real")
		link := filepath.Join(dir, "link")
		require.NoError(t, os.WriteFile(real, []byte("old\n"), 0o644))
		require.NoError(t, os.Symlink("real", link))
		require.NoError(t, os.Lchown(link, 65534, 65534))

		assert.ErrorContains(t, WriteFileAtomic(link, []byte("new\n"), 0o644), "will not follow symlink")
		assert.Equal(t, "old\n", read(t, real))
	})
}

func TestMayFollowSymlink(t *testing.T) {
	const me, other = 1000, 2000

	assert.True(t, mayFollowSymlink(me, me), "a link the effective user owns")
	assert.True(t, mayFollowSymlink(0, 0), "root following its own link")
	assert.True(t, mayFollowSymlink(0, me), "a link root owns")
	assert.False(t, mayFollowSymlink(other, me), "a link another user owns")
	assert.False(t, mayFollowSymlink(other, 0), "a user's link, followed by root, even in the user's own directory")
}

func TestResolveSymlinkParents(t *testing.T) {
	t.Run("a parent symlink the effective user owns is followed", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "real"), 0o755))
		require.NoError(t, os.Symlink("real", filepath.Join(dir, "link")))

		require.NoError(t, WriteFileAtomic(filepath.Join(dir, "link", "file"), []byte("x\n"), 0o644))
		got, err := os.ReadFile(filepath.Join(dir, "real", "file"))
		require.NoError(t, err)
		assert.Equal(t, "x\n", string(got))
	})

	t.Run("a .. after a symlink goes up from where the link leads", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "data", "subdir"), 0o755))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "srv"), 0o755))
		require.NoError(t, os.Symlink("../data/subdir", filepath.Join(dir, "srv", "link")))

		got, err := ResolveSymlink(dir + "/srv/link/../app.conf")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, "data", "app.conf"), got)
	})

	t.Run("a link reached through a parent link's target is checked", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("changing a link's owner needs root")
		}

		// app -> current is root's, but current -> secret belongs to
		// another user, and is only reached through app
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "secret"), 0o755))
		current := filepath.Join(dir, "current")
		require.NoError(t, os.Symlink("secret", current))
		require.NoError(t, os.Lchown(current, 65534, 65534))
		require.NoError(t, os.Symlink(current, filepath.Join(dir, "app")))

		assert.ErrorContains(t, WriteFileAtomic(filepath.Join(dir, "app", "file"), []byte("x\n"), 0o644), "will not follow symlink "+current)
		assert.NoFileExists(t, filepath.Join(dir, "secret", "file"))
	})

	t.Run("a parent symlink another user owns is refused", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("changing a link's owner needs root")
		}

		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "real"), 0o755))
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink("real", link))
		require.NoError(t, os.Lchown(link, 65534, 65534))

		assert.ErrorContains(t, WriteFileAtomic(filepath.Join(link, "file"), []byte("x\n"), 0o644), "will not follow symlink")
		assert.NoFileExists(t, filepath.Join(dir, "real", "file"))
	})
}

func TestWriteAtomic(t *testing.T) {
	t.Run("streams the content", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")

		require.NoError(t, WriteAtomic(path, 0o600, func(w io.Writer) error {
			_, err := io.Copy(w, strings.NewReader("streamed\n"))
			return err
		}))

		b, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "streamed\n", string(b))
	})

	t.Run("an error from write leaves the original and no temporary file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file")
		require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o644))

		boom := errors.New("boom")

		err := WriteAtomic(path, 0o644, func(w io.Writer) error {
			_, _ = io.WriteString(w, "partial")
			return boom
		})
		assert.ErrorIs(t, err, boom)

		b, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "old\n", string(b))

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Len(t, entries, 1)
	})

	t.Run("writing in place calls write once, and an error leaves the file", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can write to a read-only directory")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "file")
		require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o644))
		require.NoError(t, os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		boom := errors.New("boom")
		assert.ErrorIs(t, WriteAtomic(path, 0o644, func(w io.Writer) error {
			_, _ = io.WriteString(w, "partial")
			return boom
		}), boom)

		b, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "old\n", string(b))

		calls := 0
		require.NoError(t, WriteAtomic(path, 0o644, func(w io.Writer) error {
			calls++
			_, err := io.WriteString(w, "new\n")
			return err
		}))

		b, err = os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "new\n", string(b))
		assert.Equal(t, 1, calls)
	})
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}

	return false
}

func TestResolveIn(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "etc"), 0o755))
	require.NoError(t, os.Symlink("/etc/real", filepath.Join(root, "etc", "absolute")))
	require.NoError(t, os.Symlink("../../../etc/real", filepath.Join(root, "etc", "climbing")))

	t.Run("an absolute link starts again from the root", func(t *testing.T) {
		got, err := resolveIn(root, filepath.Join(root, "etc", "absolute"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "etc", "real"), got)
	})

	t.Run("a relative link cannot climb above the root", func(t *testing.T) {
		got, err := resolveIn(root, filepath.Join(root, "etc", "climbing"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "etc", "real"), got)
	})

	t.Run("a path outside the root is an error", func(t *testing.T) {
		_, err := resolveIn(root, "/etc/passwd")
		assert.ErrorContains(t, err, "is not under")
	})

	t.Run("a link reached after a missing path is still followed", func(t *testing.T) {
		require.NoError(t, os.Symlink("missing/../escape", filepath.Join(root, "entry")))
		require.NoError(t, os.Symlink("/etc/real", filepath.Join(root, "escape")))

		got, err := resolveIn(root, filepath.Join(root, "entry", "file"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "etc", "real", "file"), got)
	})

	t.Run("missing components are joined on as they are", func(t *testing.T) {
		got, err := resolveIn(root, filepath.Join(root, "missing", "a", "..", "b"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "missing", "b"), got)
	})
}

func TestEnvResolveSymlink(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "etc"), 0o755))
	require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(root, "etc", "link")))

	t.Run("an absolute link stays under Root", func(t *testing.T) {
		got, err := Env{Root: root}.ResolveSymlink("/etc/link")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "etc", "passwd"), got)
	})

	t.Run("without Root it is ResolveSymlink", func(t *testing.T) {
		got, err := Env{}.ResolveSymlink(filepath.Join(root, "etc", "link"))
		require.NoError(t, err)
		assert.Equal(t, "/etc/passwd", got)
	})
}
