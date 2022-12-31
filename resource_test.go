package viaduct

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetKind(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		attr     ResourceAttributes
		expected ResourceKind
	}{
		{
			attr:     &Apt{},
			expected: "Apt",
		},
		{
			attr:     &Directory{},
			expected: "Directory",
		},
		{
			attr:     &Execute{},
			expected: "Execute",
		},
		{
			attr:     &File{},
			expected: "File",
		},
		{
			attr:     &Git{},
			expected: "Git",
		},
		{
			attr:     &Link{},
			expected: "Link",
		},
		{
			attr:     &Package{},
			expected: "Package",
		},
	} {
		var r Resource
		err := r.setKind(test.attr)
		assert.NoError(t, err)

		assert.Equal(t, test.expected, r.ResourceKind)
	}

}

func TestSetID(t *testing.T) {
	t.Parallel()

	r := Resource{}

	err := r.setKind(&File{})
	assert.NoError(t, err)

	err = r.setID()
	assert.NoError(t, err)

	assert.Equal(t, ResourceID("File_id-3d542aff"), r.ResourceID)
}

func TestNewReso(t *testing.T) {
	t.Parallel()

	t.Run("error if invalid dependency", func(t *testing.T) {
		t.Parallel()

		_, err := newResource([]*Resource{{}})
		assert.Error(t, err)
	})
}
