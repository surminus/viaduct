package viaduct

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type testResourceType struct {
	Value    string
	WithLock bool
}

func (t *testResourceType) OperationName() string {
	return "Test"
}

func (t *testResourceType) Params() *ResourceParams {
	if t.WithLock {
		return NewResourceParamsWithLock()
	}

	return NewResourceParams()
}

func (t *testResourceType) PreflightChecks(log *Logger) error {
	return nil
}

func (t *testResourceType) Run(log *Logger) error {
	return nil
}

func newTestResource(value string) *testResourceType {
	return &testResourceType{Value: value}
}

func newTestResourceWithLock(value string) *testResourceType {
	return &testResourceType{Value: value, WithLock: true}
}

var testResource = newTestResource("test")

func TestSetKind(t *testing.T) {
	t.Parallel()

	var r Resource
	err := r.setKind(testResource)
	assert.NoError(t, err)

	assert.Equal(t, ResourceKind("testResourceType"), r.ResourceKind)
}

func TestSetID(t *testing.T) {
	t.Parallel()

	r := Resource{}

	err := r.setKind(testResource)
	assert.NoError(t, err)

	err = r.setID()
	assert.NoError(t, err)

	assert.Equal(t, ResourceID("testResourceType_id-274da5a8"), r.ResourceID)
}

func TestNewResource(t *testing.T) {
	t.Parallel()

	t.Run("error if invalid dependency", func(t *testing.T) {
		t.Parallel()

		_, err := newResource([]*Resource{{}})
		assert.Error(t, err)
	})
}
