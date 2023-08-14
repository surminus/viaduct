package resources

import (
	"os"
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
