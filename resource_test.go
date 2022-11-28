package viaduct

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetKind(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		attr     any
		expected ResourceKind
	}{
		{
			attr:     Apt{},
			expected: KindApt,
		},
		{
			attr:     Directory{},
			expected: KindDirectory,
		},
		{
			attr:     Execute{},
			expected: KindExecute,
		},
		{
			attr:     File{},
			expected: KindFile,
		},
		{
			attr:     Git{},
			expected: KindGit,
		},
		{
			attr:     Link{},
			expected: KindLink,
		},
		{
			attr:     Package{},
			expected: KindPackage,
		},
	} {
		var r Resource
		err := r.setKind(test.attr)
		assert.NoError(t, err)

		assert.Equal(t, test.expected, r.ResourceKind)
	}

}

func TestCheckAllowedOperations(t *testing.T) {
	t.Parallel()

	t.Run("allowed operations", func(t *testing.T) {
		t.Parallel()

		r := Resource{
			Operation:    OperationCreate,
			ResourceKind: KindFile,
		}

		assert.NoError(t, r.checkAllowedOperation())
	})

	t.Run("execute for create not allowed", func(t *testing.T) {
		t.Parallel()

		r := Resource{
			Operation:    OperationCreate,
			ResourceKind: KindExecute,
		}

		assert.Error(t, r.checkAllowedOperation())
	})

	t.Run("execute for delete not allowed", func(t *testing.T) {
		t.Parallel()

		r := Resource{
			Operation:    OperationDelete,
			ResourceKind: KindExecute,
		}

		assert.Error(t, r.checkAllowedOperation())
	})
}

func TestSetID(t *testing.T) {
	t.Parallel()

	// Just use an empty resource
	r := Resource{}

	err := r.setID()
	assert.NoError(t, err)

	assert.Equal(t, ResourceID("7f15fd974e5fa5682684d6cb54d56291e2299230"), r.ResourceID)
}

func TestNewReso(t *testing.T) {
	t.Parallel()

	t.Run("error if invalid dependency", func(t *testing.T) {
		t.Parallel()

		_, err := newResource(OperationCreate, []*Resource{{}})
		assert.Error(t, err)
	})

}
