package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

// Creating groups requires root, so we only test validation and helpers
// here. See package_test.go for thoughts on acceptance testing.

func TestGroupPreflightChecks(t *testing.T) {
	t.Run("requires name", func(t *testing.T) {
		g := &Group{}

		err := g.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Name")
	})
}

func TestSystemGroup(t *testing.T) {
	g := SystemGroup("node_exporter")

	assert.Equal(t, "node_exporter", g.Name)
	assert.True(t, g.System)
}

func TestGroupOperationName(t *testing.T) {
	assert.Equal(t, "Create", (&Group{Name: "test"}).OperationName())
	assert.Equal(t, "Delete", (&Group{Name: "test", Delete: true}).OperationName())
}

func TestGroupParams(t *testing.T) {
	params := (&Group{Name: "test"}).Params()

	assert.True(t, params.GlobalLock)
	assert.Equal(t, viaduct.PasswdLock, params.LockKey)
}

func TestGroupExistingGIDConflict(t *testing.T) {
	// The root group exists everywhere with gid 0, so asking for a different
	// gid has to be an error rather than a silent noop
	g := &Group{Name: "root", GID: 12345}

	err := g.create(testLogger)
	assert.EqualError(t, err, "group root exists with gid 0, not 12345")
}
