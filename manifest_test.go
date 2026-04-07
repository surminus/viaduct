package viaduct

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetName(t *testing.T) {
	t.Parallel()

	m := New()
	r := m.Add(testResource)
	m.SetName(r, "test-name")

	expected := map[ResourceID]Resource{
		ResourceID("test-name"): {
			Attributes:   testResource,
			ResourceID:   "test-name",
			Status:       Pending,
			ResourceKind: ResourceKind("testResourceType"),
		},
	}

	assert.Equal(t, m.resources, expected)
}

func TestSetDep(t *testing.T) {
	t.Parallel()

	m := New()
	r := m.Add(testResource)
	m.SetDep(r, "test-dep")

	expected := map[ResourceID]Resource{
		r.ResourceID: {
			Attributes:   testResource,
			DependsOn:    []ResourceID{"test-dep"},
			ResourceID:   r.ResourceID,
			Status:       Pending,
			ResourceKind: ResourceKind("testResourceType"),
		},
	}

	assert.Equal(t, expected, m.resources)
}

func TestWithLock(t *testing.T) {
	t.Parallel()

	m := New()
	r := m.Add(testResource)
	m.WithLock(r)

	expected := map[ResourceID]Resource{
		r.ResourceID: {
			Attributes:   testResource,
			GlobalLock:   true,
			ResourceID:   r.ResourceID,
			Status:       Pending,
			ResourceKind: ResourceKind("testResourceType"),
		},
	}

	assert.Equal(t, expected, m.resources)
}

func TestAddResource(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		m := New()
		r, err := newResource([]*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = r.setKind(testResource)
		assert.NoError(t, err)

		err = m.addResource(r, testResource)
		assert.NoError(t, err)

		assert.Equal(t, 1, len(m.resources))

		for id, res := range m.resources {
			// Set ResourceKind happens separately to this function,
			// but should it?
			expected := Resource{
				Attributes:   testResource,
				DependsOn:    []ResourceID{ResourceID("test")},
				ResourceID:   id,
				Status:       Pending,
				ResourceKind: "testResourceType",
			}

			assert.Equal(t, expected, res)
		}
	})

	t.Run("error if resource repeated", func(t *testing.T) {
		t.Parallel()

		m := New()
		r, err := newResource([]*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = r.setKind(testResource)
		assert.NoError(t, err)

		err = m.addResource(r, testResource)
		assert.NoError(t, err)

		sameAttributes := newTestResource("test")
		sameResource, err := newResource([]*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = m.addResource(sameResource, sameAttributes)
		assert.Error(t, err)
	})

	t.Run("automatic global lock", func(t *testing.T) {
		t.Parallel()

		m := New()
		p := newTestResourceWithLock("test")
		r, err := newResource([]*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = r.setKind(p)
		assert.NoError(t, err)

		err = m.addResource(r, p)
		assert.NoError(t, err)

		for id, res := range m.resources {
			expected := Resource{
				Attributes:   p,
				DependsOn:    []ResourceID{ResourceID("test")},
				GlobalLock:   true,
				ResourceID:   id,
				Status:       Pending,
				ResourceKind: ResourceKind("testResourceType"),
			}

			assert.Equal(t, expected, res)
		}
	})
}

func TestCollectFailures(t *testing.T) {
	t.Parallel()

	t.Run("single root failure with no dependents", func(t *testing.T) {
		t.Parallel()

		resources := map[ResourceID]Resource{
			"file-1": {
				ResourceID:   "file-1",
				ResourceKind: "File",
				Status:       Failed,
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("permission denied"), Message: "permission denied"},
			},
			"git-1": {
				ResourceID:   "git-1",
				ResourceKind: "Git",
				Status:       Success,
				Attributes:   testResource,
			},
		}

		failures := collectFailures(resources)
		assert.Len(t, failures, 1)
		assert.Equal(t, "file-1", failures[0].ResourceID)
		assert.Equal(t, "test", failures[0].Description)
		assert.Equal(t, "permission denied", failures[0].Error)
		assert.Empty(t, failures[0].Dependents)
	})

	t.Run("root failure with dependency failures", func(t *testing.T) {
		t.Parallel()

		resources := map[ResourceID]Resource{
			"file-1": {
				ResourceID:   "file-1",
				ResourceKind: "File",
				Status:       Failed,
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("permission denied"), Message: "permission denied"},
			},
			"git-1": {
				ResourceID:   "git-1",
				ResourceKind: "Git",
				Status:       DependencyFailed,
				DependsOn:    []ResourceID{"file-1"},
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("upstream dependency file-1 returned an error"), Message: "upstream dependency file-1 returned an error"},
			},
			"exec-1": {
				ResourceID:   "exec-1",
				ResourceKind: "Execute",
				Status:       DependencyFailed,
				DependsOn:    []ResourceID{"file-1"},
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("upstream dependency file-1 returned an error"), Message: "upstream dependency file-1 returned an error"},
			},
		}

		failures := collectFailures(resources)
		assert.Len(t, failures, 1)
		assert.Equal(t, "file-1", failures[0].ResourceID)
		assert.Len(t, failures[0].Dependents, 2)
	})

	t.Run("transitive dependency failure traces to root", func(t *testing.T) {
		t.Parallel()

		resources := map[ResourceID]Resource{
			"file-1": {
				ResourceID:   "file-1",
				ResourceKind: "File",
				Status:       Failed,
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("permission denied"), Message: "permission denied"},
			},
			"git-1": {
				ResourceID:   "git-1",
				ResourceKind: "Git",
				Status:       DependencyFailed,
				DependsOn:    []ResourceID{"file-1"},
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("upstream"), Message: "upstream"},
			},
			"exec-1": {
				ResourceID:   "exec-1",
				ResourceKind: "Execute",
				Status:       DependencyFailed,
				DependsOn:    []ResourceID{"git-1"},
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("upstream"), Message: "upstream"},
			},
		}

		failures := collectFailures(resources)
		assert.Len(t, failures, 1)
		assert.Equal(t, "file-1", failures[0].ResourceID)
		assert.Len(t, failures[0].Dependents, 2)
	})

	t.Run("multiple independent root failures", func(t *testing.T) {
		t.Parallel()

		resources := map[ResourceID]Resource{
			"file-1": {
				ResourceID:   "file-1",
				ResourceKind: "File",
				Status:       Failed,
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("error 1"), Message: "error 1"},
			},
			"file-2": {
				ResourceID:   "file-2",
				ResourceKind: "File",
				Status:       Failed,
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("error 2"), Message: "error 2"},
			},
		}

		failures := collectFailures(resources)
		assert.Len(t, failures, 2)
	})

	t.Run("no failures returns empty", func(t *testing.T) {
		t.Parallel()

		resources := map[ResourceID]Resource{
			"file-1": {
				ResourceID:   "file-1",
				ResourceKind: "File",
				Status:       Success,
				Attributes:   testResource,
			},
		}

		failures := collectFailures(resources)
		assert.Empty(t, failures)
	})
}
