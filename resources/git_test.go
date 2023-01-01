package resources

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

func newTestGit(t *testing.T, path string) *Git {
	g := &Git{
		Path:  path,
		Quiet: true,
		URL:   "https://github.com/surminus/viaduct",
	}

	err := g.PreflightChecks(testLogger)
	if err != nil {
		t.Fatal(err)
	}

	return g
}

func TestGit(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		g := newTestGit(t, "test/acceptance/git/create")

		err := g.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.DirExists(filepath.Join(g.Path, ".git")))

		// Use the same test for cloning and deleting so we don't waste time
		// cloning twice
		g.Delete = true
		err = g.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, false, viaduct.DirExists(g.Path))
	})
}
