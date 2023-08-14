package resources

import (
	"os"
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
