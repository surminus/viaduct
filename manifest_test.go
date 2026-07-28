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

func TestChain(t *testing.T) {
	t.Parallel()

	t.Run("wires each link to the previous one", func(t *testing.T) {
		t.Parallel()

		m := New()
		chain := m.Chain(
			newTestResource("a"),
			newTestResource("b"),
			newTestResource("c"),
		)

		assert.Len(t, chain, 3)

		// The first link has no dependencies.
		assert.Empty(t, chain[0].DependsOn)

		// Each subsequent link depends only on the one before it.
		assert.Equal(t, []ResourceID{chain[0].ResourceID}, chain[1].DependsOn)
		assert.Equal(t, []ResourceID{chain[1].ResourceID}, chain[2].DependsOn)
	})

	t.Run("empty chain returns no resources", func(t *testing.T) {
		t.Parallel()

		m := New()
		chain := m.Chain()

		assert.Empty(t, chain)
		assert.Empty(t, m.resources)
	})

	t.Run("single link has no dependencies", func(t *testing.T) {
		t.Parallel()

		m := New()
		chain := m.Chain(newTestResource("only"))

		assert.Len(t, chain, 1)
		assert.Empty(t, chain[0].DependsOn)
	})
}

func TestChainFrom(t *testing.T) {
	t.Parallel()

	t.Run("first link depends on the starting resource", func(t *testing.T) {
		t.Parallel()

		m := New()
		base := m.Add(newTestResource("base"))
		chain := m.ChainFrom(base,
			newTestResource("a"),
			newTestResource("b"),
		)

		assert.Len(t, chain, 2)
		assert.Equal(t, []ResourceID{base.ResourceID}, chain[0].DependsOn)
		assert.Equal(t, []ResourceID{chain[0].ResourceID}, chain[1].DependsOn)
	})

	t.Run("nil starting resource behaves like Chain", func(t *testing.T) {
		t.Parallel()

		m := New()
		chain := m.ChainFrom(nil,
			newTestResource("a"),
			newTestResource("b"),
		)

		assert.Len(t, chain, 2)
		assert.Empty(t, chain[0].DependsOn)
		assert.Equal(t, []ResourceID{chain[0].ResourceID}, chain[1].DependsOn)
	})
}

func TestChainTo(t *testing.T) {
	t.Parallel()

	t.Run("target depends on the last link", func(t *testing.T) {
		t.Parallel()

		m := New()
		target := m.Add(newTestResource("target"))
		chain := m.ChainTo(target,
			newTestResource("a"),
			newTestResource("b"),
		)

		assert.Len(t, chain, 2)
		assert.Empty(t, chain[0].DependsOn)
		assert.Equal(t, []ResourceID{chain[0].ResourceID}, chain[1].DependsOn)

		// The target is not part of the returned chain, but it now depends
		// on the chain's last link.
		stored := m.resources[target.ResourceID]
		assert.Equal(t, []ResourceID{chain.Last().ResourceID}, stored.DependsOn)
	})

	t.Run("nil target behaves like Chain", func(t *testing.T) {
		t.Parallel()

		m := New()
		chain := m.ChainTo(nil,
			newTestResource("a"),
			newTestResource("b"),
		)

		assert.Len(t, chain, 2)
		assert.Empty(t, chain[0].DependsOn)
		assert.Equal(t, []ResourceID{chain[0].ResourceID}, chain[1].DependsOn)
	})
}

func TestResourceChain(t *testing.T) {
	t.Parallel()

	t.Run("first and last return the ends of the chain", func(t *testing.T) {
		t.Parallel()

		m := New()
		chain := m.Chain(
			newTestResource("a"),
			newTestResource("b"),
			newTestResource("c"),
		)

		assert.Equal(t, chain[0], chain.First())
		assert.Equal(t, chain[2], chain.Last())
	})

	t.Run("first and last are nil for an empty chain", func(t *testing.T) {
		t.Parallel()

		var chain ResourceChain

		assert.Nil(t, chain.First())
		assert.Nil(t, chain.Last())
	})
}

func TestDependencyCycle(t *testing.T) {
	t.Parallel()

	t.Run("no cycle in a chain", func(t *testing.T) {
		t.Parallel()

		m := New()
		m.Chain(
			newTestResource("a"),
			newTestResource("b"),
			newTestResource("c"),
		)

		assert.NoError(t, m.dependencyCycle())
	})

	t.Run("detects a cycle between two resources", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"), a)
		m.SetDep(a, string(b.ResourceID))

		err := m.dependencyCycle()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dependency cycle detected")
		assert.Contains(t, err.Error(), string(a.ResourceID))
		assert.Contains(t, err.Error(), string(b.ResourceID))
	})

	t.Run("detects a resource depending on itself", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.Add(newTestResource("a"))
		m.SetDep(a, string(a.ResourceID))

		assert.Error(t, m.dependencyCycle())
	})

	t.Run("ignores dependencies that are not in the manifest", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.Add(newTestResource("a"))
		m.SetDep(a, "does-not-exist")

		assert.NoError(t, m.dependencyCycle())
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

	t.Run("dependency failure with no root is listed on its own", func(t *testing.T) {
		t.Parallel()

		// A resource that gave up waiting has no failed dependency to blame,
		// so it has to be reported in its own right
		resources := map[ResourceID]Resource{
			"file-1": {
				ResourceID:   "file-1",
				ResourceKind: "File",
				Status:       Success,
				Attributes:   testResource,
			},
			"exec-1": {
				ResourceID:   "exec-1",
				ResourceKind: "Execute",
				Status:       DependencyFailed,
				DependsOn:    []ResourceID{"file-1"},
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("gave up waiting"), Message: "gave up waiting"},
			},
		}

		failures := collectFailures(resources)
		assert.Len(t, failures, 1)
		assert.Equal(t, "exec-1", failures[0].ResourceID)
		assert.Equal(t, "gave up waiting", failures[0].Error)
	})

	t.Run("error without a status is still reported", func(t *testing.T) {
		t.Parallel()

		// Whether a run failed and what gets reported come from the same
		// place, so a resource cannot fail a run without being named
		resources := map[ResourceID]Resource{
			"exec-1": {
				ResourceID:   "exec-1",
				ResourceKind: "Execute",
				Status:       Pending,
				Attributes:   testResource,
				Error:        Error{Err: fmt.Errorf("something went wrong"), Message: "something went wrong"},
			},
		}

		failures := collectFailures(resources)
		assert.Len(t, failures, 1)
		assert.Equal(t, "exec-1", failures[0].ResourceID)
		assert.Equal(t, "something went wrong", failures[0].Error)
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
