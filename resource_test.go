package viaduct

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testResourceType struct {
	Value    string
	WithLock bool
	LockKey  LockKey

	// block holds Run until it is released, standing in for a resource that
	// takes longer than its timeout allows
	block     chan struct{}
	blockOnce sync.Once

	// ran records whether the operation was started at all
	ran atomic.Bool
}

func (t *testResourceType) Description() string {
	return t.Value
}

func (t *testResourceType) OperationName() string {
	return "Test"
}

func (t *testResourceType) Params() *ResourceParams {
	if t.LockKey != "" {
		return NewResourceParamsWithLockKey(t.LockKey)
	}

	if t.WithLock {
		return NewResourceParamsWithLock()
	}

	return NewResourceParams()
}

func (t *testResourceType) PreflightChecks(log *Logger) error {
	return nil
}

func (t *testResourceType) Run(log *Logger) error {
	t.ran.Store(true)

	if t.block != nil {
		<-t.block
	}

	return nil
}

// release lets a blocking test resource finish
func (t *testResourceType) release() {
	t.blockOnce.Do(func() { close(t.block) })
}

func newTestResource(value string) *testResourceType {
	return &testResourceType{Value: value}
}

func newTestResourceWithLock(value string) *testResourceType {
	return &testResourceType{Value: value, WithLock: true}
}

func newTestResourceWithLockKey(value string, key LockKey) *testResourceType {
	return &testResourceType{Value: value, LockKey: key}
}

// newBlockingTestResource returns a resource whose Run does not finish until
// release is called
func newBlockingTestResource(value string) *testResourceType {
	return &testResourceType{Value: value, block: make(chan struct{})}
}

var testResource = newTestResource("test")

func TestSetKind(t *testing.T) {
	t.Parallel()

	var r Resource
	err := r.setKind(testResource)
	assert.NoError(t, err)

	assert.Equal(t, ResourceKind("testResourceType"), r.ResourceKind)
}

func TestHashID(t *testing.T) {
	t.Parallel()

	r := Resource{}
	require.NoError(t, r.setKind(testResource))

	assert.Contains(t, string(r.hashID()), "testResourceType_id-")
}
