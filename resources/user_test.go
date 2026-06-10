package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Creating users requires root, so we only test validation and helpers
// here. See package_test.go for thoughts on acceptance testing.

func TestUserPreflightChecks(t *testing.T) {
	t.Run("requires name", func(t *testing.T) {
		u := &User{}

		err := u.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Name")
	})
}

func TestSystemUser(t *testing.T) {
	u := SystemUser("node_exporter")

	assert.Equal(t, "node_exporter", u.Name)
	assert.True(t, u.System)
	assert.Equal(t, "/bin/false", u.Shell)
}
