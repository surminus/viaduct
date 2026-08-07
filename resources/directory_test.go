package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

func newTestDirectory(t *testing.T, path string) *Directory {
	d := &Directory{Path: path}

	err := d.PreflightChecks(testLogger)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func TestDirectory(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		d := newTestDirectory(t, "test/acceptance/directory/test_create_directory")

		err := d.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.DirExists(d.Path))
		assert.Equal(t, true, viaduct.MatchChmod(d.Path, DefaultDirectoryPermissions))

		err = os.RemoveAll(d.Path)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("no recursive chown", func(t *testing.T) {
		t.Parallel()

		if os.Geteuid() == 0 {
			t.Skip("root walks a directory regardless of its permissions")
		}

		path := "test/acceptance/directory/test_no_recursive_chown"
		// A path the ownership cannot be applied to stands in for the real
		// case, such as a read-only mount
		unreachable := filepath.Join(path, "unreachable")

		err := os.MkdirAll(unreachable, 0o755)
		if err != nil {
			t.Fatal(err)
		}

		err = os.Chmod(unreachable, 0o000)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() {
			_ = os.Chmod(unreachable, 0o755)
			_ = os.RemoveAll(path)
		})

		recursive := newTestDirectory(t, path)
		assert.Error(t, recursive.Run(testLogger))

		shallow := DirShallow(path)
		err = shallow.PreflightChecks(testLogger)
		if err != nil {
			t.Fatal(err)
		}

		assert.NoError(t, shallow.Run(testLogger))
		assert.Equal(t, true, viaduct.DirExists(path))
		assert.Equal(t, true, viaduct.MatchChmod(path, DefaultDirectoryPermissions))
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		d := newTestDirectory(t, "test/acceptance/directory/test_delete_directory")
		d.Delete = true

		err := os.MkdirAll(d.Path, 0o755)
		if err != nil {
			t.Fatal(err)
		}

		err = d.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, false, viaduct.DirExists(d.Path))
	})
}
