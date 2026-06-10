package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Applying sysctl values requires root, so we only test validation and
// content generation here.

func TestSysctlPreflightChecks(t *testing.T) {
	t.Run("requires name", func(t *testing.T) {
		s := &Sysctl{Values: map[string]string{"vm.swappiness": "10"}}

		err := s.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Name")
	})

	t.Run("requires values", func(t *testing.T) {
		s := &Sysctl{Name: "99-test"}

		err := s.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Values")
	})
}

func TestSysctlContent(t *testing.T) {
	s := &Sysctl{
		Name: "99-test",
		Values: map[string]string{
			"vm.swappiness":    "10",
			"fs.file-max":      "65536",
			"vm.max_map_count": "262144",
		},
	}

	expected := "fs.file-max = 65536\nvm.max_map_count = 262144\nvm.swappiness = 10\n"
	assert.Equal(t, expected, s.content())
}
