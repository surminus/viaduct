package viaduct

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingResource fails its preflight checks or its run
type failingResource struct {
	testResourceType
	preflightErr error
	runErr       error
}

func (f *failingResource) PreflightChecks(_ context.Context, log *Logger) error {
	return f.preflightErr
}

func (f *failingResource) Run(_ context.Context, log *Logger) error {
	f.ran.Store(true)
	return f.runErr
}

func TestApply(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		m := New()
		a := m.Add(newTestResource("a"))
		m.Add(newTestResource("b"), a)

		report, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		require.Len(t, report.Results, 2)
		assert.Equal(t, a.ResourceID, report.Results[0].ID, "a finishes before its dependant")

		for _, r := range report.Results {
			assert.Equal(t, StatusSuccess, r.Status)
			assert.NoError(t, r.Err)
			assert.False(t, r.Start.IsZero())
		}
	})

	t.Run("a duplicate resource is an error rather than an exit", func(t *testing.T) {
		m := New()
		m.Add(newTestResource("a"))
		dup := m.Add(newTestResource("a"))
		assert.NotNil(t, dup)

		ran := newTestResource("b")
		m.Add(ran)

		report, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrInvalidManifest)
		assert.ErrorContains(t, err, "is already in the manifest")
		assert.Empty(t, report.Results)
		assert.False(t, ran.ran.Load(), "nothing runs when the manifest is invalid")
	})

	t.Run("depending on an invalid resource is reported once", func(t *testing.T) {
		m := New()
		m.Add(newTestResource("a"))
		dup := m.Add(newTestResource("a"))
		m.Add(newTestResource("b"), dup)
		m.Add(newTestResource("c"), dup)

		_, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrInvalidManifest)
		assert.Len(t, m.errs, 1)
	})

	t.Run("a nil dependency is an error", func(t *testing.T) {
		m := New()
		m.Add(newTestResource("a"), nil)

		_, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrInvalidManifest)
		assert.ErrorContains(t, err, "depends on a nil resource")
	})

	t.Run("naming an unknown resource is an error", func(t *testing.T) {
		m := New()
		other := New().Add(newTestResource("a"))
		m.SetName(other, "a")

		_, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrInvalidManifest)
	})

	t.Run("preflight failures", func(t *testing.T) {
		bad := &failingResource{testResourceType: testResourceType{Value: "bad"}, preflightErr: errors.New("missing Path")}
		good := newTestResource("good")

		m := New()
		r := m.Add(bad)
		m.Add(good)

		report, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrPreflightFailed)
		assert.ErrorContains(t, err, string(r.ResourceID))
		assert.False(t, good.ran.Load(), "nothing runs when a preflight fails")

		require.Len(t, report.Results, 1)
		assert.Equal(t, r.ResourceID, report.Results[0].ID)
		assert.Equal(t, StatusFailed, report.Results[0].Status)
		assert.Equal(t, "Preflight", report.Results[0].Operation)
		assert.EqualError(t, report.Results[0].Err, "missing Path")
	})

	t.Run("run failures include the failed resource and its dependants", func(t *testing.T) {
		bad := &failingResource{testResourceType: testResourceType{Value: "bad"}, runErr: errors.New("boom")}

		m := New()
		a := m.Add(bad)
		b := m.Add(newTestResource("b"), a)

		report, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrRunFailed)

		byID := map[ResourceID]Result{}
		for _, r := range report.Results {
			byID[r.ID] = r
		}

		assert.Equal(t, StatusFailed, byID[a.ResourceID].Status)
		assert.EqualError(t, byID[a.ResourceID].Err, "boom")
		assert.Equal(t, StatusDependencyFailed, byID[b.ResourceID].Status)
		assert.Equal(t, a.ResourceID, byID[b.ResourceID].Cause)
		assert.ErrorContains(t, byID[b.ResourceID].Err, "upstream dependency failingResource bad ("+string(a.ResourceID)+") failed")
	})

	t.Run("a cancelled context stops resources starting", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		later := newTestResource("a")

		m := New()
		m.Add(later)

		report, err := m.Apply(ctx, Options{})
		assert.ErrorIs(t, err, ErrRunFailed)
		assert.False(t, later.ran.Load())
		require.Len(t, report.Results, 1)
		assert.ErrorIs(t, report.Results[0].Err, context.Canceled)
	})

	t.Run("a resource queued on a lock does not start once cancelled", func(t *testing.T) {
		holder := &startedResource{started: make(chan struct{})}
		holder.LockKey = PackageLock

		gate := &waitingResource{wait: holder.started, done: make(chan struct{})}
		queued := newTestResourceWithLockKey("queued", PackageLock)

		m := New()
		m.Add(holder)
		q := m.Add(queued, m.Add(gate))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			// queued is past its dependency and waiting for the lock holder
			<-gate.done
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		report, err := m.Apply(ctx, Options{})
		require.ErrorIs(t, err, ErrRunFailed)

		assert.False(t, queued.ran.Load())

		res := byID(report)[q.ResourceID]
		assert.Equal(t, StatusDependencyFailed, res.Status)
		assert.ErrorIs(t, res.Err, context.Canceled)
		assert.ErrorContains(t, res.Err, "not started")
		assert.Zero(t, res.Attempt)
		assert.True(t, res.Start.IsZero())
		assert.Empty(t, res.Cause)
	})

	t.Run("the temporary directory is removed when a run fails", func(t *testing.T) {
		var dir string

		bad := &failingResource{testResourceType: testResourceType{Value: "bad"}}
		bad.runErr = errors.New("boom")

		m := New()
		m.Add(&tmpUser{dir: &dir})
		m.Add(bad)

		_, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrRunFailed)
		require.NotEmpty(t, dir)
		assert.NoDirExists(t, dir)
	})

	t.Run("can run twice", func(t *testing.T) {
		var dir string

		m := New()
		m.Add(&tmpUser{dir: &dir})

		_, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		m2 := New()
		m2.Add(&tmpUser{dir: &dir})

		_, err = m2.Apply(context.Background(), Options{})
		require.NoError(t, err)
	})

	t.Run("the same manifest applied twice gives independent reports", func(t *testing.T) {
		flaky := &flakyResource{failures: 1}

		m := New()
		f := m.Add(flaky)
		after := m.Add(newTestResource("after"), f)

		first, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrRunFailed)

		second, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err, "nothing from the first run carries over")

		require.Len(t, first.Results, 2)
		assert.Equal(t, StatusDependencyFailed, byID(first)[after.ResourceID].Status)

		require.Len(t, second.Results, 2)
		assert.Equal(t, StatusSuccess, byID(second)[f.ResourceID].Status)
		assert.Equal(t, StatusSuccess, byID(second)[after.ResourceID].Status)
	})
}

// waitingResource waits for a channel to close, then closes its own
type waitingResource struct {
	testResourceType
	wait <-chan struct{}
	done chan struct{}
}

func (w *waitingResource) Run(_ context.Context, log *Logger) error {
	<-w.wait
	close(w.done)

	return nil
}

// tmpUser creates the temporary directory, recording where it was
type tmpUser struct {
	testResourceType
	dir *string
}

func (u *tmpUser) Run(_ context.Context, log *Logger) error {
	dir, err := TmpDir()
	*u.dir = dir
	return err
}

// changingResource logs a change
type changingResource struct {
	testResourceType
}

func (c *changingResource) Run(_ context.Context, log *Logger) error {
	log.Info("changed", "value", c.Value)
	return nil
}

func TestApplyResults(t *testing.T) {
	t.Run("changed is reported per resource", func(t *testing.T) {
		m := New()
		changes := m.Add(&changingResource{testResourceType{Value: "a"}})
		quiet := m.Add(newTestResource("b"))

		report, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		byID := map[ResourceID]Result{}
		for _, r := range report.Results {
			byID[r.ID] = r
		}

		assert.True(t, byID[changes.ResourceID].Changed)
		assert.False(t, byID[quiet.ResourceID].Changed)
	})

	t.Run("OnResult is called once per resource, never concurrently", func(t *testing.T) {
		m := New()
		for i := range 50 {
			m.Add(newTestResource(fmt.Sprintf("r-%d", i)))
		}

		var (
			running atomic.Int32
			overlap atomic.Bool
			got     []Result
		)

		report, err := m.Apply(context.Background(), Options{
			OnResult: func(r Result) {
				if running.Add(1) > 1 {
					overlap.Store(true)
				}

				got = append(got, r)
				time.Sleep(time.Millisecond)

				running.Add(-1)
			},
		})
		require.NoError(t, err)

		assert.False(t, overlap.Load())
		assert.Equal(t, report.Results, got)
		assert.Empty(t, report.Failed())
	})

	t.Run("events are attributed to the resource", func(t *testing.T) {
		rec := &recorder{}

		m := New()
		r := m.Add(&changingResource{testResourceType{Value: "a"}})

		_, err := m.Apply(context.Background(), Options{Handler: rec})
		require.NoError(t, err)

		var found bool
		for _, e := range rec.all() {
			if e.Level == LevelChanged {
				found = true
				assert.Equal(t, r.ResourceID, e.ResourceID)
				assert.Equal(t, ResourceKind("changingResource"), e.Kind)
				assert.Equal(t, "Test", e.Operation)
				assert.Equal(t, "a", e.Description)
			}
		}

		assert.True(t, found)
		assert.Contains(t, rec.messages(LevelInfo), "started")
	})

	t.Run("compile errors are logged", func(t *testing.T) {
		rec := &recorder{}

		m := New()
		m.Add(newTestResource("a"))
		m.Add(newTestResource("a"))

		_, err := m.Apply(context.Background(), Options{Handler: rec})
		require.ErrorIs(t, err, ErrInvalidManifest)

		errs := rec.messages(LevelError)
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "is already in the manifest")
	})

	t.Run("a dependency on an unknown name is an error", func(t *testing.T) {
		m := New()
		r := m.Add(newTestResource("a"))
		m.SetDep(r, "missing")

		_, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrInvalidManifest)
		assert.ErrorContains(t, err, "missing")
	})
}
