package resources

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

func newTestLink(t *testing.T, source, dest string) *Link {
	d := &Link{Source: source, Path: dest}

	err := d.PreflightChecks(testLogger)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func TestLink(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		l := newTestLink(t, "test/acceptance/link/source.txt", "test/acceptance/link/create.txt")

		err := l.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.LinkExists(l.Path))
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		l := newTestLink(t, "test/acceptance/link/source.txt", "test/acceptance/link/delete.txt")
		l.Delete = true

		err := os.Symlink(viaduct.ExpandPath(l.Source), viaduct.ExpandPath(l.Path))
		if err != nil {
			t.Fatal(err)
		}

		err = l.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, false, viaduct.LinkExists(l.Path))
	})
}
