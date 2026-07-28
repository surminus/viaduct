package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

func newTestFile(t *testing.T, path string) *File {
	f := &File{
		Path:    path,
		Content: "Test Content",
	}

	err := f.PreflightChecks(testLogger)
	if err != nil {
		t.Fatal(err)
	}

	return f
}

func TestFile(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		f := newTestFile(t, "test/acceptance/file/create.txt")

		err := f.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.FileExists(f.Path))
		assert.Equal(t, true, viaduct.MatchChmod(f.Path, DefaultFilePermissions))
		assert.Equal(t, f.Content, viaduct.FileContents(f.Path))

		err = os.Remove(f.Path)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("create with missing parent dir", func(t *testing.T) {
		t.Parallel()

		dir := "test/acceptance/file/createp"
		f := CreateFileP(dir+"/nested/create.txt", "Test Content")

		err := f.PreflightChecks(testLogger)
		assert.NoError(t, err)

		// Clear any leftover state from an interrupted previous run so the
		// precondition below is reliable.
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}

		// The parent directory should not exist yet.
		assert.Equal(t, false, viaduct.DirExists(dir))

		err = f.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.FileExists(f.Path))
		assert.Equal(t, f.Content, viaduct.FileContents(f.Path))

		err = os.RemoveAll(dir)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("permissions only leaves the content alone", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "unmanaged.txt")
		if err := os.WriteFile(path, []byte("not ours to write\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		f := SetPermissions(path, 0o600)
		if err := f.PreflightChecks(testLogger); err != nil {
			t.Fatal(err)
		}

		err := f.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, "not ours to write\n", viaduct.FileContents(path))
		assert.Equal(t, true, viaduct.MatchChmod(path, 0o600))
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
		if err := f.PreflightChecks(testLogger); err != nil {
			t.Fatal(err)
		}

		assert.NoError(t, f.Run(testLogger))

		after, err := fileOwnership(path)
		assert.NoError(t, err)
		assert.Equal(t, before, after)
		assert.Equal(t, true, viaduct.MatchChmod(path, 0o640))
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
		// without root, and the group must come through untouched
		f := &File{Path: path, PermissionsOnly: true, Permissions: Permissions{UID: before.uid}}
		if err := f.PreflightChecks(testLogger); err != nil {
			t.Fatal(err)
		}

		assert.NoError(t, f.Run(testLogger))

		after, err := fileOwnership(path)
		assert.NoError(t, err)
		assert.Equal(t, before.gid, after.gid)

		// The mode was not asked for, so it is left as it was
		assert.Equal(t, true, viaduct.MatchChmod(path, 0o644))
	})

	t.Run("permissions only errors when the file is missing", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "missing.txt")

		f := SetPermissions(path, 0o600)
		if err := f.PreflightChecks(testLogger); err != nil {
			t.Fatal(err)
		}

		err := f.Run(testLogger)
		assert.EqualError(t, err, "file does not exist: "+path)
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		f := newTestFile(t, "test/acceptance/file/delete.txt")
		f.Delete = true

		_, err := os.Create(f.Path)
		if err != nil {
			t.Fatal(err)
		}

		err = f.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, false, viaduct.FileExists(f.Path))
	})
}

func TestFilePreflightChecks(t *testing.T) {
	t.Parallel()

	t.Run("requires path", func(t *testing.T) {
		t.Parallel()

		err := (&File{}).PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Path")
	})

	t.Run("rejects content with permissions only", func(t *testing.T) {
		t.Parallel()

		f := &File{Path: "/tmp/test", Content: "hello", PermissionsOnly: true}

		err := f.PreflightChecks(testLogger)
		assert.EqualError(t, err, "cannot set both Content and PermissionsOnly")
	})

	t.Run("rejects delete with permissions only", func(t *testing.T) {
		t.Parallel()

		f := &File{Path: "/tmp/test", Delete: true, PermissionsOnly: true}

		err := f.PreflightChecks(testLogger)
		assert.EqualError(t, err, "cannot set both Delete and PermissionsOnly")
	})

	t.Run("permissions only needs something to manage", func(t *testing.T) {
		t.Parallel()

		f := &File{Path: "/tmp/test", PermissionsOnly: true}

		err := f.PreflightChecks(testLogger)
		assert.EqualError(t, err, "PermissionsOnly needs one of Mode, User, Group, UID or GID")
	})

	t.Run("permissions only does not default the mode", func(t *testing.T) {
		t.Parallel()

		f := &File{Path: "/tmp/test", PermissionsOnly: true, Permissions: Permissions{User: "root"}}

		assert.NoError(t, f.PreflightChecks(testLogger))
		assert.Zero(t, f.Mode)
	})
}

func TestFileOperationName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Create", (&File{Path: "/tmp/test"}).OperationName())
	assert.Equal(t, "Delete", DeleteFile("/tmp/test").OperationName())
	assert.Equal(t, "Update", SetPermissions("/tmp/test", 0o600).OperationName())
}
