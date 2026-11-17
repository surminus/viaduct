package viaduct

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyResource fails until it has been tried enough times
type flakyResource struct {
	testResourceType
	failures int
	err      error
	params   *ResourceParams

	attempts atomic.Int32
}

func (f *flakyResource) Params() *ResourceParams {
	if f.params != nil {
		return f.params
	}

	return f.testResourceType.Params()
}

func (f *flakyResource) Run(ctx context.Context, log *Logger) error {
	n := int(f.attempts.Add(1))
	if n <= f.failures {
		if f.err != nil {
			return f.err
		}

		return fmt.Errorf("attempt %d failed", n)
	}

	log.Info("done")

	return nil
}

func fastRetry(attempts int) RetryPolicy {
	return RetryPolicy{Attempts: attempts, Initial: time.Millisecond, Max: 5 * time.Millisecond}
}

func TestRetryPolicy(t *testing.T) {
	t.Run("backoff grows to the maximum, with jitter", func(t *testing.T) {
		p := RetryPolicy{Attempts: 10, Initial: 100 * time.Millisecond, Max: time.Second}

		for attempt, base := range map[int]time.Duration{
			1: 100 * time.Millisecond,
			2: 200 * time.Millisecond,
			3: 400 * time.Millisecond,
			4: 800 * time.Millisecond,
			5: time.Second,
			9: time.Second,
		} {
			for range 20 {
				d := p.backoff(attempt)
				assert.GreaterOrEqual(t, d, base/2, "attempt %d", attempt)
				assert.LessOrEqual(t, d, base, "attempt %d", attempt)
			}
		}
	})

	t.Run("defaults", func(t *testing.T) {
		p := RetryPolicy{Attempts: 3}.withDefaults()
		assert.Equal(t, time.Second, p.Initial)
		assert.Equal(t, 30*time.Second, p.Max)
	})

	t.Run("Max limits the default Initial", func(t *testing.T) {
		p := RetryPolicy{Attempts: 3, Max: 100 * time.Millisecond}

		assert.Equal(t, 100*time.Millisecond, p.withDefaults().Initial)

		for attempt := 1; attempt <= 3; attempt++ {
			assert.LessOrEqual(t, p.backoff(attempt), 100*time.Millisecond)
		}
	})

	t.Run("does not overflow", func(t *testing.T) {
		p := RetryPolicy{Attempts: 100, Initial: time.Second, Max: time.Minute}
		assert.LessOrEqual(t, p.backoff(90), time.Minute)
		assert.Positive(t, p.backoff(90))
	})
}

func TestPermanent(t *testing.T) {
	base := errors.New("base")
	err := Permanent(base)

	assert.True(t, IsPermanent(err))
	assert.True(t, IsPermanent(fmt.Errorf("wrapped: %w", err)))
	assert.ErrorIs(t, err, base)
	assert.EqualError(t, err, "base")
	assert.False(t, IsPermanent(base))
	assert.Nil(t, Permanent(nil))
}

func TestRetry(t *testing.T) {
	t.Run("retries until it succeeds", func(t *testing.T) {
		r := &flakyResource{failures: 2}

		m := New()
		res := m.Add(r)
		m.WithRetry(res, fastRetry(3))

		var results []Result
		report, err := m.Apply(context.Background(), Options{OnResult: func(r Result) { results = append(results, r) }})
		require.NoError(t, err)

		assert.EqualValues(t, 3, r.attempts.Load())
		require.Len(t, results, 3)
		assert.Equal(t, report.Results, results)

		for i, res := range results {
			assert.Equal(t, i+1, res.Attempt)
			assert.Equal(t, i == 2, res.Final)
		}

		assert.Equal(t, StatusFailed, results[0].Status)
		assert.EqualError(t, results[0].Err, "attempt 1 failed")
		assert.Equal(t, StatusSuccess, results[2].Status)
		assert.True(t, results[2].Changed)

		final := report.Final()
		require.Len(t, final, 1)
		assert.Equal(t, 3, final[0].Attempt)
		assert.Empty(t, report.Failed())
	})

	t.Run("gives up after the last attempt", func(t *testing.T) {
		r := &flakyResource{failures: 10}

		m := New()
		res := m.Add(r)
		m.WithRetry(res, fastRetry(3))

		report, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrRunFailed)
		assert.EqualValues(t, 3, r.attempts.Load())

		failed := report.Failed()
		require.Len(t, failed, 1)
		assert.Equal(t, 3, failed[0].Attempt)
		assert.EqualError(t, failed[0].Err, "attempt 3 failed")
	})

	t.Run("a permanent error stops retrying", func(t *testing.T) {
		r := &flakyResource{failures: 10, err: Permanent(errors.New("never going to work"))}

		m := New()
		res := m.Add(r)
		m.WithRetry(res, fastRetry(5))

		_, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrRunFailed)
		assert.EqualValues(t, 1, r.attempts.Load())
	})

	t.Run("max elapsed stops retrying", func(t *testing.T) {
		r := &flakyResource{failures: 100}

		m := New()
		res := m.Add(r)
		m.WithRetry(res, RetryPolicy{Attempts: 100, Initial: 20 * time.Millisecond, Max: 20 * time.Millisecond, MaxElapsed: 50 * time.Millisecond})

		start := time.Now()
		_, err := m.Apply(context.Background(), Options{})
		assert.ErrorIs(t, err, ErrRunFailed)
		assert.Less(t, int(r.attempts.Load()), 10)
		assert.Less(t, time.Since(start), time.Second)
	})

	t.Run("cancelling the run stops the backoff", func(t *testing.T) {
		r := &flakyResource{failures: 100}

		m := New()
		res := m.Add(r)
		m.WithRetry(res, RetryPolicy{Attempts: 100, Initial: time.Hour, Max: time.Hour})

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		report, err := m.Apply(ctx, Options{})
		assert.ErrorIs(t, err, ErrRunFailed)
		assert.EqualValues(t, 1, r.attempts.Load())
		assert.Less(t, time.Since(start), 5*time.Second)

		// The failure reported is that of the last attempt
		failed := report.Failed()
		require.Len(t, failed, 1)
		assert.Equal(t, 1, failed[0].Attempt)
		assert.ErrorIs(t, failed[0].Err, context.DeadlineExceeded)
		assert.Len(t, report.Results, 2, "the attempt, then the final result once stopped")
	})

	t.Run("dependants wait for the final attempt", func(t *testing.T) {
		r := &flakyResource{failures: 2}
		after := newTestResource("after")

		m := New()
		res := m.Add(r)
		m.Add(after, res)
		m.WithRetry(res, fastRetry(3))

		_, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)
		assert.True(t, after.ran.Load())
	})

	t.Run("the lock is released between attempts", func(t *testing.T) {
		spans := &spans{}

		flaky := &lockedFlaky{spans: spans}
		other := &lockedOther{spans: spans}

		m := New()
		f := m.Add(flaky)
		m.WithLockKey(f, PackageLock)
		m.WithRetry(f, RetryPolicy{Attempts: 2, Initial: 100 * time.Millisecond, Max: 100 * time.Millisecond})

		// other is ready while flaky's first attempt holds the lock, and
		// queues for it
		gate := m.Add(&sleeper{d: 10 * time.Millisecond})
		m.Add(other, gate)

		_, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		first, second, ran := spans.get("flaky-1"), spans.get("flaky-2"), spans.get("other")

		assert.False(t, ran.start.Before(first.end), "other waited for the lock flaky held")
		assert.False(t, second.start.Before(ran.end), "other ran while flaky backed off")
	})

	t.Run("an abandoned resource is not retried", func(t *testing.T) {
		blocked := newBlockingTestResource("a")
		defer blocked.release()

		m := New()
		r := m.Add(blocked)
		m.WithTimeout(r, 20*time.Millisecond)
		m.WithRetry(r, fastRetry(3))

		report, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrRunFailed)

		require.Len(t, report.Results, 1)
		assert.ErrorIs(t, report.Results[0].Err, errAbandoned)
	})

	t.Run("a resource that times out and returns is retried", func(t *testing.T) {
		r := &timingOut{}

		m := New()
		res := m.Add(r)
		m.WithTimeout(res, 20*time.Millisecond)
		m.WithRetry(res, fastRetry(3))

		report, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		assert.EqualValues(t, 2, r.attempts.Load())
		require.Len(t, report.Results, 2)
		assert.ErrorIs(t, report.Results[0].Err, context.DeadlineExceeded)
		assert.Equal(t, StatusSuccess, report.Results[1].Status)
	})
}

// timingOut overruns its timeout on the first attempt, stopping when its
// context is cancelled, and succeeds on the next
type timingOut struct {
	testResourceType
	attempts atomic.Int32
}

func (r *timingOut) Run(ctx context.Context, log *Logger) error {
	if r.attempts.Add(1) == 1 {
		<-ctx.Done()
		return ctx.Err()
	}

	return nil
}

// span is when something started and ended
type span struct {
	start, end time.Time
}

// spans records spans by name from resources running concurrently
type spans struct {
	mu sync.Mutex
	m  map[string]span
}

func (s *spans) record(name string, fn func()) {
	start := time.Now()
	fn()
	end := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.m == nil {
		s.m = map[string]span{}
	}

	s.m[name] = span{start: start, end: end}
}

func (s *spans) get(name string) span {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.m[name]
}

// sleeper takes a while
type sleeper struct {
	testResourceType
	d time.Duration
}

func (s *sleeper) Run(ctx context.Context, log *Logger) error {
	time.Sleep(s.d)
	return nil
}

// lockedFlaky fails its first attempt, slowly
type lockedFlaky struct {
	testResourceType
	spans    *spans
	attempts atomic.Int32
}

func (l *lockedFlaky) Run(ctx context.Context, log *Logger) error {
	n := l.attempts.Add(1)

	l.spans.record(fmt.Sprintf("flaky-%d", n), func() {
		if n == 1 {
			time.Sleep(50 * time.Millisecond)
		}
	})

	if n == 1 {
		return errors.New("first attempt fails")
	}

	return nil
}

type lockedOther struct {
	testResourceType
	spans *spans
}

func (l *lockedOther) Params() *ResourceParams {
	return NewResourceParamsWithLockKey(PackageLock)
}

func (l *lockedOther) Run(ctx context.Context, log *Logger) error {
	l.spans.record("other", func() {
		time.Sleep(10 * time.Millisecond)
	})

	return nil
}

// skipper skips itself
type skipper struct {
	testResourceType
}

func (s *skipper) Run(ctx context.Context, log *Logger) error {
	return Skip("not on this platform")
}

func TestSkip(t *testing.T) {
	t.Run("a skip is not a failure, and dependants run", func(t *testing.T) {
		after := newTestResource("after")

		m := New()
		s := m.Add(&skipper{})
		m.Add(after, s)

		rec := &recorder{}
		report, err := m.Apply(context.Background(), Options{Handler: rec})
		require.NoError(t, err)

		assert.True(t, after.ran.Load())

		byID := map[ResourceID]Result{}
		for _, r := range report.Results {
			byID[r.ID] = r
		}

		assert.Equal(t, StatusSkipped, byID[s.ResourceID].Status)
		assert.True(t, IsSkip(byID[s.ResourceID].Err))
		assert.EqualError(t, byID[s.ResourceID].Err, "skipped: not on this platform")
		assert.False(t, byID[s.ResourceID].Changed)
		assert.Empty(t, report.Failed())
		assert.Contains(t, rec.messages(LevelNoop), "skipped")
	})

	t.Run("a skip is not retried", func(t *testing.T) {
		r := &flakyResource{failures: 10, err: Skip("nothing to do")}

		m := New()
		res := m.Add(r)
		m.WithRetry(res, fastRetry(3))

		_, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)
		assert.EqualValues(t, 1, r.attempts.Load())
	})
}

func TestJSONCollectorRetries(t *testing.T) {
	c := newJSONCollector()

	report := Report{Results: []Result{
		{ID: "a", Status: StatusFailed, Attempt: 1},
		{ID: "a", Status: StatusFailed, Attempt: 2},
		{ID: "a", Status: StatusSuccess, Attempt: 3, Final: true},
		{ID: "b", Status: StatusSuccess, Attempt: 1, Final: true},
	}}

	got := c.resources(report)
	require.Len(t, got, 2, "one entry per resource")
	assert.Equal(t, "a", got[0].ResourceID)
	assert.Equal(t, 3, got[0].Attempt)
	assert.Equal(t, "Success", got[0].Status)
}
