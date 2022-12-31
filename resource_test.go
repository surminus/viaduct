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
			expected: KindApt,
		},
		{
			attr:     &Directory{},
			expected: KindDirectory,
		},
		{
			attr:     &Execute{},
			expected: KindExecute,
		},
		{
			attr:     &File{},
			expected: KindFile,
		},
		{
			attr:     &Git{},
			expected: KindGit,
		},
		{
			attr:     &Link{},
			expected: KindLink,
		},
		{
			attr:     &Package{},
			expected: KindPackage,
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

	r := Resource{
		ResourceKind: KindFile,
	}

	err := r.setID()
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
