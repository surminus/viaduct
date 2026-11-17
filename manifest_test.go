package viaduct

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetName(t *testing.T) {
	t.Parallel()

	t.Run("renames the caller's resource", func(t *testing.T) {
		t.Parallel()

		m := New()
		r := m.Add(newTestResource("a"))
		m.SetName(r, "test-name")

		assert.Equal(t, ResourceID("test-name"), r.ResourceID)
		assert.Same(t, r, m.resources["test-name"])
		assert.Len(t, m.resources, 1)
		assert.Empty(t, m.errs)
	})

	t.Run("dependants follow the new name", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"), a)
		m.SetName(a, "renamed")

		require.NoError(t, m.compile())
		assert.Equal(t, []ResourceID{"renamed"}, b.DependsOn)
	})

	t.Run("names are unique", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"))
		m.SetName(a, "same")
		m.SetName(b, "same")

		require.Len(t, m.errs, 1)
		assert.EqualError(t, m.errs[0], "a resource named same already exists")
	})
}

func TestAddNamed(t *testing.T) {
	t.Parallel()

	t.Run("uses the name as the ID", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.AddNamed("first", newTestResource("a"))
		b := m.AddNamed("second", newTestResource("b"), a)

		assert.Equal(t, ResourceID("first"), a.ResourceID)
		assert.Equal(t, ResourceID("second"), b.ResourceID)
		assert.Equal(t, []ResourceID{"first"}, b.DependsOn)
		assert.Empty(t, m.errs)
	})

	t.Run("the same attributes can be added under different names", func(t *testing.T) {
		t.Parallel()

		m := New()
		m.AddNamed("one", newTestResource("a"))
		m.AddNamed("two", newTestResource("a"))

		assert.Empty(t, m.errs)
	})

	t.Run("a duplicate name is an error", func(t *testing.T) {
		t.Parallel()

		m := New()
		m.AddNamed("one", newTestResource("a"))
		dup := m.AddNamed("one", newTestResource("b"))

		require.Len(t, m.errs, 1)
		assert.EqualError(t, m.errs[0], "a resource named one already exists")
		assert.True(t, dup.invalid)
	})

	t.Run("an empty name is an error", func(t *testing.T) {
		t.Parallel()

		m := New()
		m.AddNamed("", newTestResource("a"))

		assert.Len(t, m.errs, 1)
	})
}

func TestSetDep(t *testing.T) {
	t.Parallel()

	t.Run("by name", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.AddNamed("a", newTestResource("a"))
		b := m.Add(newTestResource("b"))
		m.SetDep(b, "a")

		require.NoError(t, m.compile())
		assert.Equal(t, []ResourceID{a.ResourceID}, b.DependsOn)
	})

	t.Run("an unknown name is an error", func(t *testing.T) {
		t.Parallel()

		m := New()
		b := m.Add(newTestResource("b"))
		m.SetDep(b, "does-not-exist")

		err := m.compile()
		assert.ErrorContains(t, err, "depends on does-not-exist, which is not in this manifest")
	})

	t.Run("a resource from another manifest is an error", func(t *testing.T) {
		t.Parallel()

		other := New().Add(newTestResource("a"))

		m := New()
		m.Add(newTestResource("b"), other)

		require.Len(t, m.errs, 1)
		assert.ErrorContains(t, m.errs[0], "which is not in this manifest")
	})

	t.Run("a resource that was never added is an error", func(t *testing.T) {
		t.Parallel()

		m := New()
		m.Add(newTestResource("b"), &Resource{})

		assert.Len(t, m.errs, 1)
	})
}

func TestWithLock(t *testing.T) {
	t.Parallel()

	m := New()
	r := m.Add(newTestResource("a"))
	m.WithLockKey(r, PackageLock)
	m.WithLock(r)

	assert.True(t, r.GlobalLock)
	assert.Empty(t, r.LockKey, "escalating to the keyless lock drops the key")
}

func TestWithLockKey(t *testing.T) {
	t.Parallel()

	m := New()
	r := m.Add(newTestResource("a"))
	m.WithLockKey(r, PackageLock)

	assert.True(t, r.GlobalLock)
	assert.Equal(t, PackageLock, r.LockKey)
}

func TestMethodsOnUnknownResources(t *testing.T) {
	t.Parallel()

	other := New().Add(newTestResource("a"))

	m := New()
	m.WithLock(other)
	m.WithLockKey(other, PackageLock)
	m.WithTimeout(other, time.Second)
	m.SetDep(other, "x")
	m.SetName(other, "x")
	m.WithLock(nil)

	assert.Len(t, m.errs, 6)
}

func TestAdd(t *testing.T) {
	t.Parallel()

	t.Run("returns the stored resource", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.Add(newTestResource("a"))
		r := m.Add(newTestResource("b"), a)

		assert.Same(t, r, m.resources[r.ResourceID])
		assert.Equal(t, ResourceKind("testResourceType"), r.ResourceKind)
		assert.Equal(t, []ResourceID{a.ResourceID}, r.DependsOn)
		assert.Contains(t, string(r.ResourceID), "testResourceType_id-")
	})

	t.Run("generated IDs are stable", func(t *testing.T) {
		t.Parallel()

		a := New().Add(newTestResource("a"))
		b := New().Add(newTestResource("a"))

		assert.Equal(t, a.ResourceID, b.ResourceID)
	})

	t.Run("error if resource repeated", func(t *testing.T) {
		t.Parallel()

		m := New()
		m.Add(newTestResource("a"))
		m.Add(newTestResource("a"))

		require.Len(t, m.errs, 1)
		assert.EqualError(t, m.errs[0], "testResourceType a is already in the manifest; use AddNamed to add it under another name")
	})

	t.Run("resources differing only in what JSON cannot show get their own IDs", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.Add(&hiddenResource{hidden: "a"})
		b := m.Add(&hiddenResource{hidden: "b"})
		c := m.Add(&hiddenResource{hidden: "c"})

		assert.Empty(t, m.errs)
		assert.Equal(t, a.ResourceID+"-2", b.ResourceID)
		assert.Equal(t, a.ResourceID+"-3", c.ResourceID)
	})

	t.Run("attributes that cannot be marshalled", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.Add(&funcResource{f: func() {}})
		b := m.Add(&funcResource{f: func() {}})

		assert.Empty(t, m.errs)
		assert.NotEqual(t, a.ResourceID, b.ResourceID)
	})

	t.Run("keyed lock from params", func(t *testing.T) {
		t.Parallel()

		m := New()
		r := m.Add(newTestResourceWithLockKey("test", PasswdLock))

		assert.True(t, r.GlobalLock)
		assert.Equal(t, PasswdLock, r.LockKey)
	})

	t.Run("automatic global lock", func(t *testing.T) {
		t.Parallel()

		m := New()
		r := m.Add(newTestResourceWithLock("test"))

		assert.True(t, r.GlobalLock)
		assert.Empty(t, r.LockKey)
	})

	t.Run("nil attributes", func(t *testing.T) {
		t.Parallel()

		m := New()
		r := m.Add(nil)

		assert.True(t, r.invalid)
		assert.Len(t, m.errs, 1)
	})
}

// hiddenResource differs only in an unexported field
type hiddenResource struct {
	testResourceType
	hidden string
}

// funcResource has a field that cannot be marshalled
type funcResource struct {
	testResourceType
	F func() `json:"f"`
	f func()
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
		require.NoError(t, m.compile())
		assert.Equal(t, []ResourceID{chain.Last().ResourceID}, target.DependsOn)
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

		assert.NoError(t, m.compile())
	})

	t.Run("detects a cycle between two resources", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"), a)
		m.SetDep(a, string(b.ResourceID))

		err := m.compile()
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

		assert.Error(t, m.compile())
	})
}

func TestGeneratedIDs(t *testing.T) {
	t.Parallel()

	// Generated IDs are used in --dump-manifest, --json and SetDep, so they
	// must not change between versions
	m := New()
	a := m.Add(newTestResource("a"))
	b := m.Add(newTestResourceWithLockKey("b", PackageLock), a)
	c := m.Add(&paramsTimeoutResource{testResourceType: testResourceType{Value: "c"}, timeout: time.Minute})

	assert.Equal(t, ResourceID("testResourceType_id-81d59653"), a.ResourceID)
	assert.Equal(t, ResourceID("testResourceType_id-3f0c4ec3"), b.ResourceID)
	assert.Equal(t, time.Minute, c.Timeout)
}

// newCompiledRun returns a run of a manifest with its dependencies resolved
func newCompiledRun(t *testing.T, m *Manifest) *run {
	t.Helper()

	require.NoError(t, m.compile())

	return newRun(m, Options{})
}

func TestDependencyCheck(t *testing.T) {
	t.Run("returns immediately without dependencies", func(t *testing.T) {
		m := New()
		r := m.Add(newTestResource("a"))
		rn := newCompiledRun(t, m)

		_, err := rn.dependencyCheck(context.Background(), r)
		assert.NoError(t, err)
	})

	t.Run("waits for a dependency to succeed", func(t *testing.T) {
		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"), a)
		rn := newCompiledRun(t, m)

		go func() {
			time.Sleep(20 * time.Millisecond)
			rn.record(a, Result{ID: a.ResourceID, Status: StatusSuccess, Final: true})
		}()

		_, err := rn.dependencyCheck(context.Background(), b)
		assert.NoError(t, err)
	})

	t.Run("fails when a dependency failed, naming it", func(t *testing.T) {
		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"), a)
		rn := newCompiledRun(t, m)

		rn.record(a, Result{ID: a.ResourceID, Status: StatusFailed, Final: true})

		cause, err := rn.dependencyCheck(context.Background(), b)
		assert.Equal(t, a.ResourceID, cause)
		assert.EqualError(t, err, fmt.Sprintf("upstream dependency testResourceType a (%s) failed", a.ResourceID))
	})

	t.Run("stops waiting when the context is cancelled", func(t *testing.T) {
		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"), a)
		rn := newCompiledRun(t, m)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		cause, err := rn.dependencyCheck(ctx, b)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, cause)
	})

	t.Run("waits as long as a slow dependency takes", func(t *testing.T) {
		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(newTestResource("b"), a)

		// Each resource is bounded by its own timeout, so waiting behind a
		// slow one is never a failure however long the run takes
		m.SetResourceTimeout(10 * time.Millisecond)

		rn := newCompiledRun(t, m)

		go func() {
			time.Sleep(100 * time.Millisecond)
			rn.record(a, Result{ID: a.ResourceID, Status: StatusSuccess, Final: true})
		}()

		_, err := rn.dependencyCheck(context.Background(), b)
		assert.NoError(t, err)
	})
}

// byID returns the final result of each resource in a report
func byID(report Report) map[ResourceID]Result {
	out := map[ResourceID]Result{}
	for _, r := range report.Final() {
		out[r.ID] = r
	}

	return out
}

func TestResourceTimeout(t *testing.T) {
	t.Run("gives up on a resource that overruns", func(t *testing.T) {
		blocked := newBlockingTestResource("a")
		defer blocked.release()

		m := New()
		r := m.Add(blocked)
		m.SetResourceTimeout(20 * time.Millisecond)

		report, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrRunFailed)

		// The failure is reported against the resource that overran, rather
		// than against whatever happened to be waiting for it
		res := byID(report)[r.ResourceID]
		assert.Equal(t, StatusFailed, res.Status)
		assert.ErrorContains(t, res.Err, "timed out after 20ms")
		assert.ErrorContains(t, res.Err, "may still be running")
	})

	t.Run("dependents fail once a resource is abandoned", func(t *testing.T) {
		blocked := newBlockingTestResource("a")
		defer blocked.release()

		m := New()
		a := m.Add(blocked)
		b := m.Add(newTestResource("b"), a)
		m.SetResourceTimeout(20 * time.Millisecond)

		report, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrRunFailed)

		results := byID(report)
		assert.Equal(t, StatusFailed, results[a.ResourceID].Status)
		assert.Equal(t, StatusDependencyFailed, results[b.ResourceID].Status)
		assert.Equal(t, a.ResourceID, results[b.ResourceID].Cause)

		// Both are reported, with the overrun as the root of the failure
		failures := collectFailures(report.Final())
		require.Len(t, failures, 1)
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

		require.NoError(t, m.compile())
		rn := newRun(m, Options{})

		// One after the other, so b is only ready once a has been abandoned
		rn.apply(context.Background(), a)
		rn.apply(context.Background(), b)

		// The abandoned operation is still running and still holds whatever it
		// locked, so the rest of the run does not go near it
		assert.False(t, later.ran.Load())

		res := rn.states[b].final
		assert.Equal(t, StatusDependencyFailed, res.Status)
		assert.Equal(t, a.ResourceID, res.Cause)
		assert.ErrorContains(t, res.Err, "not started")
		assert.ErrorContains(t, res.Err, string(a.ResourceID))
	})

	t.Run("a resource within its timeout succeeds", func(t *testing.T) {
		m := New()
		r := m.Add(newTestResource("a"))
		m.SetResourceTimeout(time.Minute)

		report, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		res := byID(report)[r.ResourceID]
		assert.Equal(t, StatusSuccess, res.Status)
		assert.NoError(t, res.Err)
	})

	t.Run("a negative timeout gives a resource as long as it takes", func(t *testing.T) {
		slow := newBlockingTestResource("a")

		m := New()
		r := m.Add(slow)
		m.SetResourceTimeout(-1)

		go func() {
			time.Sleep(50 * time.Millisecond)
			slow.release()
		}()

		report, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)
		assert.Equal(t, StatusSuccess, byID(report)[r.ResourceID].Status)
	})
}

func TestTimeoutFor(t *testing.T) {
	m := New()
	r := m.Add(newTestResource("a"))

	timeout := func(o Options) time.Duration {
		return newRun(m, o).timeoutFor(r)
	}

	// The default applies when nothing has been set
	assert.Equal(t, defaultResourceTimeout, timeout(Options{}))

	// The manifest overrides the default
	m.SetResourceTimeout(time.Minute)
	assert.Equal(t, time.Minute, timeout(Options{}))

	// The resource overrides the manifest
	m.WithTimeout(r, 2*time.Minute)
	assert.Equal(t, 2*time.Minute, timeout(Options{}))

	// The option overrides everything
	assert.Equal(t, 3*time.Minute, timeout(Options{ResourceTimeout: 3 * time.Minute}))
}

func TestCollectFailures(t *testing.T) {
	t.Parallel()

	failed := func(id ResourceID, msg string) Result {
		return Result{ID: id, Kind: "File", Description: "test", Status: StatusFailed, Err: errors.New(msg), Final: true}
	}

	depFailed := func(id, cause ResourceID) Result {
		return Result{ID: id, Kind: "Execute", Description: "test", Status: StatusDependencyFailed, Err: errors.New("upstream"), Cause: cause, Final: true}
	}

	t.Run("single root failure with no dependents", func(t *testing.T) {
		t.Parallel()

		failures := collectFailures([]Result{
			failed("file-1", "permission denied"),
			{ID: "git-1", Kind: "Git", Status: StatusSuccess, Final: true},
		})
		require.Len(t, failures, 1)
		assert.Equal(t, "file-1", failures[0].ResourceID)
		assert.Equal(t, "test", failures[0].Description)
		assert.Equal(t, "permission denied", failures[0].Error)
		assert.Empty(t, failures[0].Dependents)
	})

	t.Run("root failure with dependency failures", func(t *testing.T) {
		t.Parallel()

		failures := collectFailures([]Result{
			depFailed("git-1", "file-1"),
			failed("file-1", "permission denied"),
			depFailed("exec-1", "file-1"),
		})
		require.Len(t, failures, 1)
		assert.Equal(t, "file-1", failures[0].ResourceID)
		require.Len(t, failures[0].Dependents, 2)
		assert.Equal(t, "exec-1", failures[0].Dependents[0].ResourceID, "dependents are sorted")
	})

	t.Run("transitive dependency failure traces to root", func(t *testing.T) {
		t.Parallel()

		failures := collectFailures([]Result{
			failed("file-1", "permission denied"),
			depFailed("git-1", "file-1"),
			depFailed("exec-1", "git-1"),
		})
		require.Len(t, failures, 1)
		assert.Equal(t, "file-1", failures[0].ResourceID)
		assert.Len(t, failures[0].Dependents, 2)
	})

	t.Run("multiple independent root failures", func(t *testing.T) {
		t.Parallel()

		failures := collectFailures([]Result{
			failed("file-2", "error 2"),
			failed("file-1", "error 1"),
		})
		require.Len(t, failures, 2)
		assert.Equal(t, "file-1", failures[0].ResourceID)
	})

	t.Run("dependency failure with no root is listed on its own", func(t *testing.T) {
		t.Parallel()

		// A resource stopped by a cancelled run has no failed dependency to
		// blame, so it has to be reported in its own right
		failures := collectFailures([]Result{
			{ID: "file-1", Status: StatusSuccess, Final: true},
			depFailed("exec-1", ""),
		})
		require.Len(t, failures, 1)
		assert.Equal(t, "exec-1", failures[0].ResourceID)
		assert.Equal(t, "upstream", failures[0].Error)
	})

	t.Run("an allowed failure is the root of what depends on it", func(t *testing.T) {
		t.Parallel()

		opt := failed("opt-1", "boom")
		opt.AllowFailure = true

		lone := failed("opt-2", "boom")
		lone.AllowFailure = true

		failures := collectFailures([]Result{opt, lone, depFailed("exec-1", "opt-1")})
		require.Len(t, failures, 1, "an allowed failure with nothing depending on it is not listed")
		assert.Equal(t, "opt-1", failures[0].ResourceID)
		require.Len(t, failures[0].Dependents, 1)
		assert.Equal(t, "exec-1", failures[0].Dependents[0].ResourceID)
	})

	t.Run("dependants allowed to fail are left out", func(t *testing.T) {
		t.Parallel()

		dep := depFailed("exec-1", "file-1")
		dep.AllowFailure = true

		failures := collectFailures([]Result{failed("file-1", "boom"), dep, depFailed("exec-2", "exec-1")})
		require.Len(t, failures, 1)
		require.Len(t, failures[0].Dependents, 1, "traced through the resource allowed to fail")
		assert.Equal(t, "exec-2", failures[0].Dependents[0].ResourceID)
	})

	t.Run("no failures returns empty", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, collectFailures([]Result{{ID: "file-1", Status: StatusSuccess, Final: true}}))
	})
}
