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

func newTestFile(t *testing.T, path string) *File {
	f := &File{
		Path:    path,
		Content: "Test Content",
	}

	err := f.PreflightChecks(testCtx, testLogger)
	if err != nil {
		t.Fatal(err)
	}

	return f
}

func TestFile(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		f := newTestFile(t, filepath.Join(t.TempDir(), "create.txt"))

		err := f.Run(testCtx, testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.FileExists(f.Path))
		assertMode(t, f.Path, DefaultFilePermissions)
		assert.Equal(t, f.Content, fileContents(t, f.Path))
	})

	t.Run("create with missing parent dir", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "createp")
		f := CreateFileP(dir+"/nested/create.txt", "Test Content")

		err := f.PreflightChecks(testCtx, testLogger)
		assert.NoError(t, err)

		err = f.Run(testCtx, testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.FileExists(f.Path))
		assert.Equal(t, f.Content, fileContents(t, f.Path))
	})

	t.Run("permissions only leaves the content alone", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "unmanaged.txt")
		if err := os.WriteFile(path, []byte("not ours to write\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		f := SetPermissions(path, 0o600)
		if err := f.PreflightChecks(testCtx, testLogger); err != nil {
			t.Fatal(err)
		}

		err := f.Run(testCtx, testLogger)
		assert.NoError(t, err)

		assert.Equal(t, "not ours to write\n", fileContents(t, path))
		assertMode(t, path, 0o600)
	})

	t.Run("permissions only leaves ownership alone", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "owned.txt")
		if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		before, err := fileOwnership(path)
		if err != nil {
			t.Fatal(err)
		}

		// Asking only for a mode must not hand the file to whoever is running,
		// which for a root-owned file would otherwise be a chown or an EPERM
		f := SetPermissions(path, 0o640)
		if err := f.PreflightChecks(testCtx, testLogger); err != nil {
			t.Fatal(err)
		}

		assert.NoError(t, f.Run(testCtx, testLogger))

		after, err := fileOwnership(path)
		assert.NoError(t, err)
		assert.Equal(t, before, after)
		assertMode(t, path, 0o640)
	})

	t.Run("permissions only keeps the group when setting the user", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "grouped.txt")
		if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		before, err := fileOwnership(path)
		if err != nil {
			t.Fatal(err)
		}

		// Setting the user to whoever is already running is a noop we can make
		// without root, and the group must come through untouched. UID 0
		// counts as unset, so root sets another user instead.
		uid := before.uid
		if uid == 0 {
			uid = 1
		}

		f := &File{Path: path, PermissionsOnly: true, Permissions: Permissions{UID: uid}}
		if err := f.PreflightChecks(testCtx, testLogger); err != nil {
			t.Fatal(err)
		}

		assert.NoError(t, f.Run(testCtx, testLogger))

		after, err := fileOwnership(path)
		assert.NoError(t, err)
		assert.Equal(t, uid, after.uid)
		assert.Equal(t, before.gid, after.gid)

		// The mode was not asked for, so it is left as it was
		assertMode(t, path, 0o644)
	})

	t.Run("permissions only errors when the file is missing", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "missing.txt")

		f := SetPermissions(path, 0o600)
		if err := f.PreflightChecks(testCtx, testLogger); err != nil {
			t.Fatal(err)
		}

		err := f.Run(testCtx, testLogger)
		assert.EqualError(t, err, "file does not exist: "+path)
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		f := newTestFile(t, filepath.Join(t.TempDir(), "delete.txt"))
		f.Delete = true

		require.NoError(t, os.WriteFile(f.Path, nil, 0o644))

		err := f.Run(testCtx, testLogger)
		assert.NoError(t, err)

		assert.Equal(t, false, viaduct.FileExists(f.Path))
	})
}

func TestFilePreflightChecks(t *testing.T) {
	t.Parallel()

	t.Run("requires path", func(t *testing.T) {
		t.Parallel()

		err := (&File{}).PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Path")
	})

	t.Run("rejects content with permissions only", func(t *testing.T) {
		t.Parallel()

		f := &File{Path: "/tmp/test", Content: "hello", PermissionsOnly: true}

		err := f.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "cannot set both Content and PermissionsOnly")
	})

	t.Run("rejects delete with permissions only", func(t *testing.T) {
		t.Parallel()

		f := &File{Path: "/tmp/test", Delete: true, PermissionsOnly: true}

		err := f.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "cannot set both Delete and PermissionsOnly")
	})

	t.Run("permissions only needs something to manage", func(t *testing.T) {
		t.Parallel()

		f := &File{Path: "/tmp/test", PermissionsOnly: true}

		err := f.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "PermissionsOnly needs one of Mode, User, Group, UID or GID")
	})

	t.Run("permissions only does not default the mode", func(t *testing.T) {
		t.Parallel()

		f := &File{Path: "/tmp/test", PermissionsOnly: true, Permissions: Permissions{User: "root"}}

		assert.NoError(t, f.PreflightChecks(testCtx, testLogger))
		assert.Zero(t, f.Mode)
	})
}

func TestFileOperationName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Create", (&File{Path: "/tmp/test"}).OperationName())
	assert.Equal(t, "Delete", DeleteFile("/tmp/test").OperationName())
	assert.Equal(t, "Update", SetPermissions("/tmp/test", 0o600).OperationName())
}

func TestFilePermissionsOnlyDryRun(t *testing.T) {
	env := viaducttest.NewEnv(t)

	log, err := env.Apply(env.DryRunContext(), SetPermissions("/opt/missing", 0o600))
	require.NoError(t, err, "a file an earlier resource would create is not an error in a dry run")
	assert.True(t, log.Changed())

	_, err = env.Apply(env.Context(), SetPermissions("/opt/missing", 0o600))
	assert.ErrorContains(t, err, "file does not exist")
}

func TestFileSource(t *testing.T) {
	t.Parallel()

	t.Run("copies the source when it runs, then is a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		f := CopyFile("/src/app.conf", "/etc/app.conf")
		f.CreateDirIfMissing = true

		env.WriteFile("/src/app.conf", "setting=1\n")

		log, err := env.Apply(env.Context(), f)
		require.NoError(t, err)
		assert.True(t, log.Changed())
		assert.Equal(t, "setting=1\n", env.ReadFile("/etc/app.conf"))

		log, err = env.Apply(env.Context(), f)
		require.NoError(t, err)
		assert.False(t, log.Changed())

		env.WriteFile("/src/app.conf", "setting=2\n")

		log, err = env.Apply(env.Context(), f)
		require.NoError(t, err)
		assert.True(t, log.Changed())
		assert.Equal(t, "setting=2\n", env.ReadFile("/etc/app.conf"))
	})

	t.Run("a missing source is a change in a dry run and an error otherwise", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), CopyFile("/src/app.conf", "/etc/app.conf"))
		require.NoError(t, err)
		assert.True(t, log.Changed())

		_, err = env.Apply(env.Context(), CopyFile("/src/app.conf", "/etc/app.conf"))
		assert.ErrorContains(t, err, "reading Source")
	})

	t.Run("preflight", func(t *testing.T) {
		t.Parallel()

		err := (&File{Path: "/a", Source: "/b", Content: "x"}).PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "cannot set both Content and Source")

		err = (&File{Path: "/a", Source: "/b", Delete: true}).PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "cannot set both Delete and Source")

		err = (&File{Path: "/a", Source: "/b", PermissionsOnly: true, Permissions: Permissions{Mode: 0o600}}).PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "cannot set both Source and PermissionsOnly")
	})
}

func TestFileAtomicWrite(t *testing.T) {
	env := viaducttest.NewEnv(t)
	env.WriteFile("/etc/app.conf", "old\n")

	before, err := os.Stat(env.Path("/etc/app.conf"))
	require.NoError(t, err)

	f := &File{Path: "/etc/app.conf", Content: "new\n", Permissions: Permissions{Mode: 0o640, UID: os.Getuid(), GID: os.Getgid()}}
	_, err = env.Apply(env.Context(), f)
	require.NoError(t, err)

	after, err := os.Stat(env.Path("/etc/app.conf"))
	require.NoError(t, err)

	assert.Equal(t, "new\n", env.ReadFile("/etc/app.conf"))
	assert.False(t, os.SameFile(before, after), "the file is replaced rather than truncated")
	assert.Equal(t, os.FileMode(0o640), after.Mode().Perm())
}

func TestFileAbsoluteSymlinkUnderRoot(t *testing.T) {
	env := viaducttest.NewEnv(t)

	// The link names a path on the host, which under Root is Root's
	host := filepath.Join(t.TempDir(), "app.conf")
	env.WriteFile(host, "old\n")
	require.NoError(t, os.MkdirAll(env.Path("/etc"), 0o755))
	require.NoError(t, os.Symlink(host, env.Path("/etc/app.conf")))

	_, err := env.Apply(env.Context(), &File{Path: "/etc/app.conf", Content: "new\n"})
	require.NoError(t, err)

	assert.Equal(t, "new\n", env.ReadFile(host))
	assert.NoFileExists(t, host)
}
