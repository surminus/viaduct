package viaduct

import (
	"fmt"
	"sync"
	"testing"
	"time"

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

func TestDependencyCheck(t *testing.T) {
	t.Run("returns immediately without dependencies", func(t *testing.T) {
		m := New()
		r := m.Add(newTestResource("a"))

		var lock sync.RWMutex
		assert.NoError(t, m.dependencyCheck(r, &lock))
	})

	t.Run("waits for a dependency to succeed", func(t *testing.T) {
		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"), a)

		var lock sync.RWMutex

		go func() {
			time.Sleep(20 * time.Millisecond)
			m.setStatus(a, &lock, Success)
		}()

		r := m.resources[b.ResourceID]
		assert.NoError(t, m.dependencyCheck(&r, &lock))
	})

	t.Run("fails when a dependency failed", func(t *testing.T) {
		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"), a)

		var lock sync.RWMutex
		m.setStatus(a, &lock, Failed)

		r := m.resources[b.ResourceID]
		err := m.dependencyCheck(&r, &lock)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "upstream dependency")
	})

	t.Run("waits as long as a slow dependency takes", func(t *testing.T) {
		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"), a)

		// Each resource is bounded by its own timeout, so waiting behind a
		// slow one is never a failure however long the run takes
		m.SetResourceTimeout(10 * time.Millisecond)

		var lock sync.RWMutex

		go func() {
			time.Sleep(100 * time.Millisecond)
			m.setStatus(a, &lock, Success)
		}()

		r := m.resources[b.ResourceID]
		assert.NoError(t, m.dependencyCheck(&r, &lock))
	})
}

func TestResourceTimeout(t *testing.T) {
	t.Run("gives up on a resource that overruns", func(t *testing.T) {
		blocked := newBlockingTestResource("a")
		defer blocked.release()

		m := New()
		r := m.Add(blocked)
		m.SetResourceTimeout(20 * time.Millisecond)

		var lock sync.RWMutex
		var wg sync.WaitGroup

		wg.Add(1)
		m.apply(*r, &wg, &lock, &sync.RWMutex{})
		wg.Wait()

		// The failure is reported against the resource that overran, rather
		// than against whatever happened to be waiting for it
		stored := m.resources[r.ResourceID]
		assert.Equal(t, Failed, stored.Status)
		assert.Contains(t, stored.Message, "timed out after 20ms")
		assert.Contains(t, stored.Message, "may still be running")
	})

	t.Run("dependents fail once a resource is abandoned", func(t *testing.T) {
		blocked := newBlockingTestResource("a")
		defer blocked.release()

		m := New()
		a := m.Add(blocked)
		b := m.Add(newTestResource("b"), a)
		m.SetResourceTimeout(20 * time.Millisecond)

		var lock sync.RWMutex
		var wg sync.WaitGroup

		wg.Add(2)
		go m.apply(m.resources[a.ResourceID], &wg, &lock, &sync.RWMutex{})
		go m.apply(m.resources[b.ResourceID], &wg, &lock, &sync.RWMutex{})
		wg.Wait()

		assert.Equal(t, Failed, m.resources[a.ResourceID].Status)
		assert.Equal(t, DependencyFailed, m.resources[b.ResourceID].Status)

		// Both are reported, with the overrun as the root of the failure
		failures := collectFailures(m.resources)
		assert.Len(t, failures, 1)
		assert.Equal(t, string(a.ResourceID), failures[0].ResourceID)
		assert.Len(t, failures[0].Dependents, 1)
	})

	t.Run("nothing else starts once a resource is abandoned", func(t *testing.T) {
		blocked := newBlockingTestResource("a")
		defer blocked.release()

		later := newTestResource("b")

		m := New()
		a := m.Add(blocked)
		b := m.Add(later)
		m.SetResourceTimeout(20 * time.Millisecond)

		var lock sync.RWMutex
		var wg sync.WaitGroup

		wg.Add(2)
		m.apply(m.resources[a.ResourceID], &wg, &lock, &sync.RWMutex{})
		m.apply(m.resources[b.ResourceID], &wg, &lock, &sync.RWMutex{})
		wg.Wait()

		// The abandoned operation is still running and still holds whatever it
		// locked, so the rest of the run does not go near it
		assert.False(t, later.ran.Load())
		assert.Equal(t, DependencyFailed, m.resources[b.ResourceID].Status)
		assert.Contains(t, m.resources[b.ResourceID].Message, "not started")
		assert.Contains(t, m.resources[b.ResourceID].Message, string(a.ResourceID))
	})

	t.Run("a resource within its timeout succeeds", func(t *testing.T) {
		m := New()
		r := m.Add(newTestResource("a"))
		m.SetResourceTimeout(time.Minute)

		var lock sync.RWMutex
		var wg sync.WaitGroup

		wg.Add(1)
		m.apply(*r, &wg, &lock, &sync.RWMutex{})
		wg.Wait()

		assert.Equal(t, Success, m.resources[r.ResourceID].Status)
		assert.NoError(t, m.resources[r.ResourceID].Err)
	})

	t.Run("a negative timeout gives a resource as long as it takes", func(t *testing.T) {
		slow := newBlockingTestResource("a")

		m := New()
		r := m.Add(slow)
		m.SetResourceTimeout(-1)

		var lock sync.RWMutex
		var wg sync.WaitGroup

		go func() {
			time.Sleep(50 * time.Millisecond)
			slow.release()
		}()

		wg.Add(1)
		m.apply(*r, &wg, &lock, &sync.RWMutex{})
		wg.Wait()

		assert.Equal(t, Success, m.resources[r.ResourceID].Status)
	})
}

func TestTimeoutFor(t *testing.T) {
	orig := Cli.ResourceTimeout
	defer func() { Cli.ResourceTimeout = orig }()

	Cli.ResourceTimeout = 0

	m := New()
	r := m.Add(newTestResource("a"))

	// The default applies when nothing has been set
	assert.Equal(t, defaultResourceTimeout, m.timeoutFor(r))

	// The manifest overrides the default
	m.SetResourceTimeout(time.Minute)
	assert.Equal(t, time.Minute, m.timeoutFor(r))

	// The resource overrides the manifest
	m.WithTimeout(r, 2*time.Minute)
	stored := m.resources[r.ResourceID]
	assert.Equal(t, 2*time.Minute, m.timeoutFor(&stored))

	// The flag overrides everything, so a slow run can be rescued without
	// recompiling the configuration
	Cli.ResourceTimeout = 3 * time.Minute
	assert.Equal(t, 3*time.Minute, m.timeoutFor(&stored))
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
