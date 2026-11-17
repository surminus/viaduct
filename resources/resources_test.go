package resources

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
)

var testLogger *viaduct.Logger

func init() {
	viaduct.Cli.SetSilent()
	testLogger = viaduct.NewLogger("Test", "Testing")
}

// assertMode checks the mode of a path
func assertMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	match, err := viaduct.MatchChmod(path, mode)
	require.NoError(t, err)
	assert.True(t, match, "mode of %s", path)
}
