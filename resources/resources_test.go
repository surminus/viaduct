package resources

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
)

var (
	testLogger *viaduct.Logger

	// testCtx runs resources on the host, as a manifest would by default
	testCtx = context.Background()
)

func init() {
	viaduct.Cli.SetSilent()
	testLogger = viaduct.NewLogger(nil, "Test", "Testing")
}

// assertMode checks the mode of a path
func assertMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	match, err := viaduct.MatchChmod(path, mode)
	require.NoError(t, err)
	assert.True(t, match, "mode of %s", path)
}

// fileContents reads a file the test expects to exist
func fileContents(t *testing.T, path string) string {
	t.Helper()

	c, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(c)
}
