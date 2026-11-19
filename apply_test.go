package viaduct

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
				assert.Equal(t, r.ResourceID, e.ID)
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

// changeOnceResource changes something and fails on its first attempt, then
// finds nothing to do
type changeOnceResource struct {
	testResourceType
	attempts atomic.Int32
}

func (c *changeOnceResource) Run(_ context.Context, log *Logger) error {
	if c.attempts.Add(1) == 1 {
		log.Info("changed")
		return errors.New("failed after changing")
	}

	log.Noop("up-to-date")

	return nil
}

func TestOnChange(t *testing.T) {
	t.Run("runs when a dependency changed", func(t *testing.T) {
		m := New()
		unchanged := m.Add(newTestResource("unchanged"))
		changed := m.Add(&changingResource{testResourceType{Value: "changed"}})
		follow := newTestResource("follow")
		m.OnChange(m.Add(follow, unchanged, changed))

		_, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		assert.True(t, follow.ran.Load())
	})

	t.Run("is skipped when no dependency changed, and its dependants run", func(t *testing.T) {
		m := New()
		dep := m.Add(newTestResource("dep"))
		follow := newTestResource("follow")
		r := m.Add(follow, dep)
		m.OnChange(r)
		after := newTestResource("after")
		m.Add(after, r)

		report, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		assert.False(t, follow.ran.Load())
		assert.True(t, after.ran.Load())

		result := report.Results[1]
		assert.Equal(t, r.ResourceID, result.ID)
		assert.Equal(t, StatusSkipped, result.Status)
		assert.True(t, IsSkip(result.Err))
		assert.EqualError(t, result.Err, "skipped: no dependency changed")
	})

	t.Run("a change made by a failed attempt counts", func(t *testing.T) {
		m := New()
		dep := m.Add(&changeOnceResource{testResourceType: testResourceType{Value: "dep"}})
		m.WithRetry(dep, fastRetry(2))
		follow := newTestResource("follow")
		m.OnChange(m.Add(follow, dep))

		_, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		assert.True(t, follow.ran.Load())
	})

	t.Run("a change in a dry run counts", func(t *testing.T) {
		m := New()
		changed := m.Add(&changingResource{testResourceType{Value: "changed"}})
		follow := newTestResource("follow")
		m.OnChange(m.Add(follow, changed))

		_, err := m.Apply(context.Background(), Options{Env: Env{DryRun: true}})
		require.NoError(t, err)

		assert.True(t, follow.ran.Load())
	})

	t.Run("needs a dependency", func(t *testing.T) {
		m := New()
		m.OnChange(m.Add(newTestResource("alone")))

		_, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrInvalidManifest)
		assert.ErrorContains(t, err, "runs on change but has no dependencies")
	})
}

func TestGroup(t *testing.T) {
	t.Run("labels results and events, and runs resources as usual", func(t *testing.T) {
		m := New()
		chain := m.Chain(
			&changingResource{testResourceType{Value: "a"}},
			&changingResource{testResourceType{Value: "b"}},
		)
		m.Group("hosts", chain...)
		m.Add(newTestResource("other"))

		var events []Event
		report, err := m.Apply(context.Background(), Options{
			Handler: HandlerFunc(func(_ context.Context, e Event) { events = append(events, e) }),
		})
		require.NoError(t, err)

		groups := map[string]string{}
		for _, r := range report.Results {
			groups[r.Description] = r.Group
		}

		assert.Equal(t, map[string]string{"a": "hosts", "b": "hosts", "other": ""}, groups)

		for _, e := range events {
			if e.Level == LevelChanged {
				assert.Equal(t, "hosts", e.Group, e.Description)
			}
		}
	})

	t.Run("reports each group once all of it has finished", func(t *testing.T) {
		m := New()
		chain := m.Chain(
			&changingResource{testResourceType{Value: "a"}},
			newTestResource("b"),
		)
		m.Group("hosts", chain...)
		m.Group("quiet", m.Add(newTestResource("c")))
		m.Add(newTestResource("other"))

		var order []string
		report, err := m.Apply(context.Background(), Options{
			OnResult: func(r Result) { order = append(order, r.Description) },
			OnGroupResult: func(g GroupResult) {
				order = append(order, "group "+g.Name)
			},
		})
		require.NoError(t, err)

		assert.Less(t, slices.Index(order, "b"), slices.Index(order, "group hosts"))
		assert.Less(t, slices.Index(order, "c"), slices.Index(order, "group quiet"))
		assert.NotContains(t, order, "group ")

		require.Len(t, report.Groups, 2)

		groups := map[string]GroupResult{}
		for _, g := range report.Groups {
			groups[g.Name] = g
		}

		hosts := groups["hosts"]
		assert.Equal(t, StatusSuccess, hosts.Status)
		assert.True(t, hosts.Changed)
		assert.False(t, hosts.AllowFailure)
		assert.Len(t, hosts.Results, 2)
		assert.False(t, hosts.Start.IsZero())
		last := hosts.Results[1]
		assert.WithinDuration(t, last.Start.Add(last.Duration), hosts.Start.Add(hosts.Duration), 0)

		assert.False(t, groups["quiet"].Changed)
	})

	t.Run("a group fails when a resource in it fails", func(t *testing.T) {
		m := New()
		a := m.Add(&failingResource{testResourceType: testResourceType{Value: "a"}, runErr: errors.New("boom")})
		b := m.Add(newTestResource("b"), a)
		m.Group("hosts", a, b)
		m.AllowFailure(a)
		m.AllowFailure(b)

		report, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		require.Len(t, report.Groups, 1)
		assert.Equal(t, StatusFailed, report.Groups[0].Status)
		assert.True(t, report.Groups[0].AllowFailure)
	})

	t.Run("a change made by a retried attempt counts", func(t *testing.T) {
		m := New()
		a := m.Add(&changeOnceResource{testResourceType: testResourceType{Value: "a"}})
		m.WithRetry(a, fastRetry(2))
		m.Group("g", a)

		var calls int
		report, err := m.Apply(context.Background(), Options{OnGroupResult: func(GroupResult) { calls++ }})
		require.NoError(t, err)

		assert.Equal(t, 1, calls)
		require.Len(t, report.Groups, 1)

		g := report.Groups[0]
		assert.Equal(t, StatusSuccess, g.Status)
		assert.True(t, g.Changed)
		require.Len(t, g.Results, 1)
		assert.Equal(t, 2, g.Results[0].Attempt)

		var first Result
		for _, r := range report.Results {
			if r.Attempt == 1 {
				first = r
			}
		}
		assert.Equal(t, first.Start, g.Start)
	})

	t.Run("a group is not reported when preflight checks fail", func(t *testing.T) {
		m := New()
		a := m.Add(&failingResource{testResourceType: testResourceType{Value: "a"}, preflightErr: errors.New("bad")})
		m.Group("g", a, m.Add(newTestResource("b")))

		report, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrPreflightFailed)
		assert.Empty(t, report.Groups)
	})

	t.Run("a group whose resources did not start is reported", func(t *testing.T) {
		m := New()
		m.Group("g", m.Add(newTestResource("a")))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		report, _ := m.Apply(ctx, Options{})
		require.Len(t, report.Groups, 1)
		assert.Equal(t, StatusDependencyFailed, report.Groups[0].Status)
		assert.True(t, report.Groups[0].Start.IsZero())
		assert.Zero(t, report.Groups[0].Duration)
	})

	t.Run("summarises statuses", func(t *testing.T) {
		at := time.Now()
		cases := []struct {
			statuses []Status
			want     Status
		}{
			{[]Status{StatusSkipped, StatusSkipped}, StatusSkipped},
			{[]Status{StatusSkipped, StatusSuccess}, StatusSuccess},
			{[]Status{StatusDependencyFailed, StatusSuccess}, StatusDependencyFailed},
			{[]Status{StatusFailed, StatusDependencyFailed}, StatusFailed},
			{[]Status{StatusDependencyFailed, StatusFailed}, StatusFailed},
		}

		for _, c := range cases {
			var members []Result
			for _, st := range c.statuses {
				members = append(members, Result{Status: st, Start: at, Final: true})
			}

			g := summarise("g", members)
			assert.Equal(t, c.want, g.Status, c.statuses)
			assert.False(t, g.AllowFailure, c.statuses)
		}
	})

	t.Run("allows failure only when every failure is allowed", func(t *testing.T) {
		failed := func(allow bool) Result {
			return Result{Status: StatusFailed, AllowFailure: allow, Final: true}
		}

		assert.False(t, summarise("g", []Result{failed(true), failed(false)}).AllowFailure)
		assert.True(t, summarise("g", []Result{failed(true), {Status: StatusSuccess, Final: true}}).AllowFailure)
		assert.True(t, summarise("g", []Result{
			failed(true),
			{Status: StatusDependencyFailed, AllowFailure: true, Final: true},
		}).AllowFailure)
	})

	t.Run("a resource can be in one group", func(t *testing.T) {
		m := New()
		a := m.Add(newTestResource("a"))
		m.Group("one", a)
		m.Group("one", a)
		m.Group("two", a)

		_, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrInvalidManifest)
		assert.ErrorContains(t, err, "is already in group one")
	})

	t.Run("needs a name", func(t *testing.T) {
		m := New()
		m.Group("", m.Add(newTestResource("a")))

		_, err := m.Apply(context.Background(), Options{})
		assert.ErrorContains(t, err, "Group: name is empty")
	})
}
