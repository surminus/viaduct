package viaduct

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrInvalidManifest is returned by Apply when the manifest could not be
	// built, such as when a resource was added twice, or the Options cannot
	// be used. Nothing has run.
	ErrInvalidManifest = errors.New("invalid manifest")

	// ErrPreflightFailed is returned by Apply when a resource failed its
	// preflight checks. Nothing has run.
	ErrPreflightFailed = errors.New("preflight checks failed")

	// ErrRunFailed is returned by Apply when one or more resources failed.
	// The report says which.
	ErrRunFailed = errors.New("run failed")
)

// Options configures Apply.
type Options struct {
	// Env is the environment resources run in. Setting Env.Root needs
	// Env.Runner too, or Apply returns an error, since commands would
	// otherwise run on the host. See Env.
	Env Env

	// Handler receives every event from the run. The default drops them.
	// NewTextHandler gives the same output as Run.
	Handler Handler

	// OnResult is called with the result of each attempt at a resource as
	// it finishes. It is never called concurrently. Optional.
	OnResult func(Result)

	// OnGroupResult is called with the result of each group made with
	// Manifest.Group, once every resource in it has its final result, after
	// OnResult is called with the last of them. A group with a resource that
	// never gets one, as when preflight checks fail, is not reported. It is
	// never called concurrently with itself or OnResult. Optional.
	OnGroupResult func(GroupResult)

	// ResourceTimeout overrides how long each resource is given to run,
	// ahead of WithTimeout and SetResourceTimeout. Zero leaves it to them,
	// and a negative duration means no timeout.
	ResourceTimeout time.Duration
}

// Report is the outcome of Apply.
type Report struct {
	// Results holds the result of each resource, in the order they
	// finished.
	Results []Result

	// Groups holds the result of each group whose resources all have a
	// final result, in the order they finished. A group with a resource that
	// never gets one, as when preflight checks fail, is left out.
	Groups []GroupResult

	// Duration is how long the run took.
	Duration time.Duration
}

// Final returns the last result of each resource, leaving out the attempts
// that were retried.
func (r Report) Final() []Result {
	var out []Result

	for _, res := range r.Results {
		if res.Final {
			out = append(out, res)
		}
	}

	return out
}

// Failed returns the final results of the resources that failed the run,
// including those that did not run because something they depend on failed.
// Resources allowed to fail are left out.
func (r Report) Failed() []Result {
	var out []Result

	for _, res := range r.Final() {
		if !res.AllowFailure && (res.Status == StatusFailed || res.Status == StatusDependencyFailed) {
			out = append(out, res)
		}
	}

	return out
}

// Result is the outcome of one attempt at a resource. A resource that is not
// retried has a single result.
type Result struct {
	ID          ResourceID
	Kind        ResourceKind
	Operation   string
	Description string
	Status      Status

	// Changed is true when the resource logged a change, or in a dry run,
	// a change it would have made.
	Changed bool

	// Err is why the resource failed, or for one that was skipped, the
	// error from Skip. It is nil otherwise.
	Err error

	// Cause is the resource whose failure stopped this one from running, for
	// a DependencyFailed result. It is empty when the run stopped for another
	// reason, such as the context being cancelled.
	Cause ResourceID

	// Attempt counts from 1. It is zero for a resource that never started.
	Attempt int

	// Final is true for the last result of a resource, and false for an
	// attempt that is retried.
	Final bool

	// AllowFailure is true for a resource whose failure does not fail the run.
	AllowFailure bool

	// Group is the name of the group the resource is in, or empty.
	Group string

	// Start is when the resource started, and is zero if it never did.
	Start time.Time

	// Duration is how long the resource took to run.
	Duration time.Duration
}

// GroupResult is the outcome of a group, from the results of the resources
// in it.
type GroupResult struct {
	// Name is the name given to Manifest.Group.
	Name string

	// Status is StatusFailed if any resource failed, otherwise
	// StatusDependencyFailed if any did not run because a dependency failed,
	// otherwise StatusSkipped if every resource was skipped, and
	// StatusSuccess if not. A failure does not fail the run when
	// AllowFailure is set.
	Status Status

	// Changed is true when any resource changed something, including on an
	// attempt that was retried.
	Changed bool

	// AllowFailure is true when every resource that failed is allowed to
	// fail.
	AllowFailure bool

	// Start is when the first attempt at any resource started, and is zero
	// if none did.
	Start time.Time

	// Duration is from Start until the last attempt finished, including any
	// time spent waiting between resources.
	Duration time.Duration

	// Results holds the final result of each resource, in the order they
	// finished.
	Results []Result
}

// summarise builds a group's result from every attempt at its resources. The
// status comes from the final results, while a change or start on an attempt
// that was retried still counts.
func summarise(name string, attempts []Result) GroupResult {
	g := GroupResult{Name: name, Status: StatusSkipped, AllowFailure: true}

	var end time.Time

	for _, r := range attempts {
		g.Changed = g.Changed || r.Changed

		if !r.Start.IsZero() {
			if g.Start.IsZero() || r.Start.Before(g.Start) {
				g.Start = r.Start
			}

			if e := r.Start.Add(r.Duration); e.After(end) {
				end = e
			}
		}

		if !r.Final {
			continue
		}

		g.Results = append(g.Results, r)

		switch r.Status {
		case StatusFailed:
			g.Status = StatusFailed
		case StatusDependencyFailed:
			if g.Status != StatusFailed {
				g.Status = StatusDependencyFailed
			}
		case StatusSuccess:
			if g.Status == StatusSkipped {
				g.Status = StatusSuccess
			}
		}

		if r.Status == StatusFailed || r.Status == StatusDependencyFailed {
			g.AllowFailure = g.AllowFailure && r.AllowFailure
		}
	}

	if g.Status != StatusFailed && g.Status != StatusDependencyFailed {
		g.AllowFailure = false
	}

	if !g.Start.IsZero() {
		g.Duration = end.Sub(g.Start)
	}

	return g
}

// results collects results from resources running concurrently, passing each
// one on to the OnResult callback, and each completed group on to
// OnGroupResult.
type results struct {
	mu       sync.Mutex
	list     []Result
	onResult func(Result)

	// remaining counts the resources in each group still without a final
	// result, and attempts holds every result of the group so far
	remaining map[string]int
	attempts  map[string][]Result
	groups    []GroupResult
	onGroup   func(GroupResult)
}

func (rs *results) add(r Result) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.list = append(rs.list, r)

	if rs.onResult != nil {
		rs.onResult(r)
	}

	if r.Group == "" {
		return
	}

	rs.attempts[r.Group] = append(rs.attempts[r.Group], r)

	if !r.Final {
		return
	}

	if rs.remaining[r.Group]--; rs.remaining[r.Group] > 0 {
		return
	}

	g := summarise(r.Group, rs.attempts[r.Group])
	rs.groups = append(rs.groups, g)

	if rs.onGroup != nil {
		rs.onGroup(g)
	}
}

func (rs *results) allGroups() []GroupResult {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	return append([]GroupResult(nil), rs.groups...)
}

func (rs *results) all() []Result {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	return append([]Result(nil), rs.list...)
}

// run is the state of a single Apply. Keeping it off the Manifest means each
// Apply starts afresh and returns its own report.
type run struct {
	m       *Manifest
	options Options
	handler Handler
	results *results
	locks   *lockSet

	// abandoned holds the first resource the run gave up on, if any.
	abandoned atomic.Pointer[Resource]

	// states is filled before any resource starts and only read afterwards,
	// so it needs no lock of its own.
	states map[*Resource]*resourceState
}

// resourceState is where a resource is in a run.
type resourceState struct {
	// done is closed once final has been set, so a waiter that sees it closed
	// can read final.
	done  chan struct{}
	final Result

	// changed is set when any attempt at the resource changed something,
	// before done is closed.
	changed bool
}

func newRun(m *Manifest, o Options) *run {
	rn := &run{
		m:       m,
		options: o,
		handler: serialise(o.Handler),
		results: &results{
			onResult:  o.OnResult,
			onGroup:   o.OnGroupResult,
			remaining: make(map[string]int),
			attempts:  make(map[string][]Result),
		},
		locks:  newLockSet(),
		states: make(map[*Resource]*resourceState, len(m.order)),
	}

	for _, r := range m.order {
		rn.states[r] = &resourceState{done: make(chan struct{})}

		if r.group != "" {
			rn.results.remaining[r.group]++
		}
	}

	return rn
}

// Apply runs the manifest, running resources concurrently once their
// dependencies have succeeded. It returns an error wrapping
// ErrInvalidManifest or ErrPreflightFailed if nothing could run, and one
// wrapping ErrRunFailed if any resource failed. The report holds a result for
// every resource that was checked or run, whatever the error.
//
// Cancelling ctx stops resources that are running and stops the rest from
// starting.
//
// A manifest can be applied more than once, and each call returns its own
// report, but not concurrently.
func (m *Manifest) Apply(ctx context.Context, o Options) (Report, error) {
	start := time.Now()

	ctx = WithEnv(ctx, o.Env)
	rn := newRun(m, o)

	if len(m.errs) > 0 {
		l := rn.logger(ctx, "Compile")
		for _, err := range m.errs {
			l.Error(err.Error())
		}

		return Report{}, fmt.Errorf("%w: %w", ErrInvalidManifest, errors.Join(m.errs...))
	}

	if o.Env.Root != "" && o.Env.Runner == nil {
		err := errors.New("the Env in Options sets Root without a Runner, so commands would run on the host")
		rn.logger(ctx, "Compile").Error(err.Error())

		return Report{}, fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}

	if _, err := LoadAttributes(); err != nil {
		rn.logger(ctx, "Run").Error("attributes", "error", err.Error())
		return Report{}, fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}

	defer holdTmpDir()()

	l := rn.logger(ctx, "Run")
	l.progress("started")

	if o.Env.DryRun {
		l.Warn("dry-run", "msg", "resources report what they would change without changing it")
	}

	l.progress("preflight-checks")

	// A cycle can never progress, so fail before any resource does work.
	if err := m.compile(); err != nil {
		l.Error("invalid-manifest", "error", err.Error())
		return Report{Duration: time.Since(start)}, fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}

	if err := rn.preflight(ctx, l); err != nil {
		return Report{Results: rn.results.all(), Groups: rn.results.allGroups(), Duration: time.Since(start)}, err
	}

	var wg sync.WaitGroup

	for _, r := range m.order {
		// A resource allowed to fail that failed preflight already has a result.
		if rn.finished(r) {
			continue
		}

		wg.Go(func() {
			rn.apply(ctx, r)
		})
	}

	wg.Wait()

	report := Report{Results: rn.results.all(), Groups: rn.results.allGroups(), Duration: time.Since(start)}

	if failed := report.Failed(); len(failed) > 0 {
		ids := make([]string, 0, len(failed))
		for _, r := range failed {
			ids = append(ids, string(r.ID))
		}

		slices.Sort(ids)

		return report, fmt.Errorf("%w: %s", ErrRunFailed, strings.Join(ids, ", "))
	}

	return report, nil
}

// logger returns a logger for events about the run itself.
func (rn *run) logger(ctx context.Context, operation string) *Logger {
	l := NewLogger(rn.handler, "Viaduct", operation)
	l.ctx = ctx

	return l
}

// newResult returns a result with the fields every result of the resource
// shares.
func (rn *run) newResult(r *Resource, operation string) Result {
	return Result{
		ID:           r.ResourceID,
		Kind:         r.ResourceKind,
		Operation:    operation,
		Description:  r.Attributes.Description(),
		AllowFailure: r.allowFailure,
		Group:        r.group,
	}
}

// preflight runs every resource's preflight checks, returning an error
// naming those that failed.
func (rn *run) preflight(ctx context.Context, l *Logger) error {
	var failed []Result

	for _, r := range rn.m.order {
		start := time.Now()
		log := newResourceLogger(ctx, rn.handler, r, "Preflight")

		err := r.Attributes.PreflightChecks(ctx, log)
		if err == nil {
			continue
		}

		result := rn.newResult(r, "Preflight")
		result.Status = StatusFailed
		result.Changed = log.Changed()
		result.Err = err
		result.Final = true
		result.Start = start
		result.Duration = time.Since(start)

		if r.allowFailure {
			log.Warn("failure-allowed", "error", err.Error())
		} else {
			failed = append(failed, result)
		}

		rn.record(r, result)
	}

	if len(failed) == 0 {
		return nil
	}

	slices.SortFunc(failed, func(a, b Result) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})

	ids := make([]string, 0, len(failed))

	for _, r := range failed {
		ids = append(ids, string(r.ID))

		l.Error("preflight-failed",
			"resource_id", string(r.ID),
			"resource_kind", string(r.Kind),
			"description", r.Description,
			"error", r.Err.Error(),
		)
	}

	return fmt.Errorf("%w: %s", ErrPreflightFailed, strings.Join(ids, ", "))
}

func (rn *run) apply(ctx context.Context, r *Resource) {
	if a := rn.abandoned.Load(); a != nil {
		rn.fail(r, abandonedErr(a), a.ResourceID)
		return
	}

	if cause, err := rn.dependencyCheck(ctx, r); err != nil {
		rn.fail(r, err, cause)
		return
	}

	if r.onChange && !rn.dependencyChanged(r) {
		rn.skip(ctx, r, "no dependency changed")
		return
	}

	var policy RetryPolicy
	if r.retry != nil {
		policy = r.retry.withDefaults()
	}

	first := time.Now()

	for attempt := 1; ; attempt++ {
		result, ran := rn.attempt(ctx, r, attempt)
		if !ran {
			return
		}

		// An attempt that changed something and then failed still counts,
		// even if a later attempt finds nothing left to do.
		if result.Changed {
			rn.states[r].changed = true
		}

		final := result.Status != StatusFailed ||
			attempt >= policy.Attempts ||
			IsPermanent(result.Err) ||
			errors.Is(result.Err, errAbandoned) ||
			ctx.Err() != nil

		var wait time.Duration
		if !final {
			wait = policy.backoff(attempt)

			if policy.MaxElapsed > 0 && time.Since(first)+wait > policy.MaxElapsed {
				final = true
			}
		}

		result.Final = final

		if final {
			rn.finish(ctx, r, result)
			return
		}

		rn.results.add(result)

		// The lock is not held while waiting.
		select {
		case <-ctx.Done():
			result.Err = fmt.Errorf("%w, and stopped before trying again: %w", result.Err, ctx.Err())
			result.Final = true
			rn.finish(ctx, r, result)

			return
		case <-time.After(wait):
		}
	}
}

// attempt runs one attempt at a resource, holding its lock. It reports false
// when the resource could not start, which has already been recorded.
func (rn *run) attempt(ctx context.Context, r *Resource, n int) (Result, bool) {
	if r.GlobalLock {
		release := rn.locks.acquire(r.LockKey)
		defer release()
	}

	// An abandoned resource releases its lock, but its operation still holds
	// the real one (dpkg's, passwd's), so waiters must check before starting.
	if a := rn.abandoned.Load(); a != nil {
		rn.fail(r, abandonedErr(a), a.ResourceID)
		return Result{}, false
	}

	if err := ctx.Err(); err != nil {
		rn.fail(r, fmt.Errorf("not started: %w", err), "")
		return Result{}, false
	}

	held := &heldLock{set: rn.locks, holding: r.GlobalLock, check: rn.notAbandoned}
	defer held.finish()

	start := time.Now()
	log := newResourceLogger(ctx, rn.handler, r, r.Attributes.OperationName())
	runErr := r.run(context.WithValue(ctx, heldLockKey{}, held), log, rn.timeoutFor(r))

	result := rn.newResult(r, r.Attributes.OperationName())
	result.Status = StatusSuccess
	result.Changed = log.Changed()
	result.Err = runErr
	result.Attempt = n
	result.Start = start
	result.Duration = time.Since(start)

	switch {
	case runErr == nil:
	case IsSkip(runErr):
		result.Status = StatusSkipped
		log.Noop("skipped", "reason", runErr.Error())
	default:
		result.Status = StatusFailed

		if errors.Is(runErr, errAbandoned) {
			// The operation is still running and the machine state is unknown,
			// so nothing else starts.
			rn.abandoned.CompareAndSwap(nil, r)
		}
	}

	return result, true
}

// finish records a resource's final result.
func (rn *run) finish(ctx context.Context, r *Resource, result Result) {
	if result.Status == StatusFailed && r.allowFailure {
		newResourceLogger(ctx, rn.handler, r, result.Operation).Warn("failure-allowed", "error", result.Err.Error())
	}

	rn.record(r, result)
}

// fail records a resource that did not run, and the resource that stopped
// it, if there was one.
func (rn *run) fail(r *Resource, err error, cause ResourceID) {
	result := rn.newResult(r, r.Attributes.OperationName())
	result.Status = StatusDependencyFailed
	result.Err = err
	result.Cause = cause
	result.Final = true

	rn.record(r, result)
}

// skip records a resource that was skipped without running.
func (rn *run) skip(ctx context.Context, r *Resource, reason string) {
	newResourceLogger(ctx, rn.handler, r, r.Attributes.OperationName()).Noop("skipped", "reason", reason)

	result := rn.newResult(r, r.Attributes.OperationName())
	result.Status = StatusSkipped
	result.Err = Skip(reason)
	result.Final = true

	rn.record(r, result)
}

// dependencyChanged reports whether any of a resource's dependencies changed
// something. It is called once they have all finished.
func (rn *run) dependencyChanged(r *Resource) bool {
	for _, d := range r.resolved {
		if rn.states[d].changed {
			return true
		}
	}

	return false
}

// record keeps a resource's final result and releases anything waiting on
// it. It is called once per resource.
func (rn *run) record(r *Resource, result Result) {
	s := rn.states[r]
	s.final = result

	rn.results.add(result)
	close(s.done)
}

// finished reports whether a resource has its final result.
func (rn *run) finished(r *Resource) bool {
	select {
	case <-rn.states[r].done:
		return true
	default:
		return false
	}
}

// notAbandoned returns abandonedErr once a resource has been abandoned.
func (rn *run) notAbandoned() error {
	if a := rn.abandoned.Load(); a != nil {
		return abandonedErr(a)
	}

	return nil
}

// abandonedErr is why nothing further may start after a resource was
// abandoned.
func abandonedErr(a *Resource) error {
	return fmt.Errorf("not started: the run gave up on %s, which may still be running", a.name())
}

// dependencyCheck blocks until every dependency of the resource has finished,
// returning an error, and the dependency to blame, if one did not succeed.
//
// The wait has no timeout of its own: every resource has one, and cycles are
// rejected before the run, so each dependency reaches a final status.
func (rn *run) dependencyCheck(ctx context.Context, r *Resource) (ResourceID, error) {
	for _, d := range r.resolved {
		select {
		case <-rn.states[d].done:
		case <-ctx.Done():
			return "", fmt.Errorf("not started: %w", ctx.Err())
		}

		if rn.settled(d) == depFailed {
			return d.ResourceID, fmt.Errorf("upstream dependency %s failed", d.name())
		}
	}

	return "", nil
}

// dependency states, as seen by a resource waiting on another
const (
	depWaiting = iota
	depSatisfied
	depFailed
)

// settled maps a dependency's final status to a dependency state. Only
// success and skipping let dependants run: an allowed failure does
// not fail the run, but what depends on it cannot rely on it either.
func (rn *run) settled(d *Resource) int {
	if !rn.finished(d) {
		return depWaiting
	}

	switch rn.states[d].final.Status {
	case StatusSuccess, StatusSkipped:
		return depSatisfied
	default:
		return depFailed
	}
}

// timeoutFor returns how long a resource is given to run. In order of
// precedence: the ResourceTimeout option, WithTimeout or the resource's
// params, the manifest, then the default. A negative duration means no
// timeout.
func (rn *run) timeoutFor(r *Resource) time.Duration {
	switch {
	case rn.options.ResourceTimeout != 0:
		return rn.options.ResourceTimeout
	case r.Timeout != 0:
		return r.Timeout
	case rn.m.resourceTimeout != 0:
		return rn.m.resourceTimeout
	default:
		return defaultResourceTimeout
	}
}

// ApplyOne runs a resource's preflight checks and then the resource itself,
// outside a manifest, logging to log. It is for a custom resource that builds
// on others once it knows a value only available part way through a run, such
// as a device path. It uses the Env in ctx and takes none of the manifest's
// locks, timeouts or retries: take a lock around it with Lock.
func ApplyOne(ctx context.Context, a ResourceAttributes, log *Logger) error {
	if err := a.PreflightChecks(ctx, log); err != nil {
		return fmt.Errorf("%s: preflight checks: %w", a.Description(), err)
	}

	return a.Run(ctx, log)
}
