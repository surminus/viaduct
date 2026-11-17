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
	// Handler receives every event from the run. The default drops them.
	// NewTextHandler gives the same output as Run.
	Handler Handler

	// OnResult is called with the result of each attempt at a resource as
	// it finishes. It is never called concurrently. Optional.
	OnResult func(Result)

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
func (r Report) Failed() []Result {
	var out []Result

	for _, res := range r.Final() {
		if res.Status == StatusFailed || res.Status == StatusDependencyFailed {
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

	// Err is why the resource failed. It is nil otherwise.
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

	// Start is when the resource started, and is zero if it never did.
	Start time.Time

	// Duration is how long the resource took to run.
	Duration time.Duration
}

// results collects results from resources running concurrently, passing each
// one on to the OnResult callback.
type results struct {
	mu       sync.Mutex
	list     []Result
	onResult func(Result)
}

func (rs *results) add(r Result) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.list = append(rs.list, r)

	if rs.onResult != nil {
		rs.onResult(r)
	}
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
}

func newRun(m *Manifest, o Options) *run {
	rn := &run{
		m:       m,
		options: o,
		handler: serialise(o.Handler),
		results: &results{onResult: o.OnResult},
		locks:   newLockSet(),
		states:  make(map[*Resource]*resourceState, len(m.order)),
	}

	for _, r := range m.order {
		rn.states[r] = &resourceState{done: make(chan struct{})}
	}

	return rn
}

// Apply runs the manifest, running resources concurrently once their
// dependencies have succeeded. It returns an error wrapping
// ErrInvalidManifest or ErrPreflightFailed if nothing could run, and one
// wrapping ErrRunFailed if any resource failed. The report holds a result for
// every resource that was checked or run, whatever the error.
//
// Cancelling ctx stops the resources that have not started from starting.
//
// A manifest can be applied more than once, and each call returns its own
// report, but not concurrently.
func (m *Manifest) Apply(ctx context.Context, o Options) (Report, error) {
	start := time.Now()

	rn := newRun(m, o)

	if len(m.errs) > 0 {
		l := rn.logger(ctx, "Compile")
		for _, err := range m.errs {
			l.Error(err.Error())
		}

		return Report{}, fmt.Errorf("%w: %w", ErrInvalidManifest, errors.Join(m.errs...))
	}

	if _, err := LoadAttributes(); err != nil {
		rn.logger(ctx, "Run").Error("attributes", "error", err.Error())
		return Report{}, fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}

	defer holdTmpDir()()

	l := rn.logger(ctx, "Run")
	l.progress("started")
	l.progress("preflight-checks")

	// A cycle can never progress, so fail before any resource does work.
	if err := m.compile(); err != nil {
		l.Error("invalid-manifest", "error", err.Error())
		return Report{Duration: time.Since(start)}, fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}

	if err := rn.preflight(ctx, l); err != nil {
		return Report{Results: rn.results.all(), Duration: time.Since(start)}, err
	}

	var wg sync.WaitGroup

	for _, r := range m.order {
		wg.Go(func() {
			rn.apply(ctx, r)
		})
	}

	wg.Wait()

	report := Report{Results: rn.results.all(), Duration: time.Since(start)}

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
		ID:          r.ResourceID,
		Kind:        r.ResourceKind,
		Operation:   operation,
		Description: r.Attributes.Description(),
	}
}

// preflight runs every resource's preflight checks, returning an error
// naming those that failed.
func (rn *run) preflight(ctx context.Context, l *Logger) error {
	var failed []Result

	for _, r := range rn.m.order {
		start := time.Now()
		log := newResourceLogger(ctx, rn.handler, r, "Preflight")

		err := r.Attributes.PreflightChecks(log)
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

		failed = append(failed, result)
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

	result, ran := rn.attempt(ctx, r, 1)
	if !ran {
		return
	}

	result.Final = true
	rn.record(r, result)
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

	start := time.Now()
	log := newResourceLogger(ctx, rn.handler, r, r.Attributes.OperationName())
	runErr := r.run(log, rn.timeoutFor(r))

	result := rn.newResult(r, r.Attributes.OperationName())
	result.Status = StatusSuccess
	result.Changed = log.Changed()
	result.Err = runErr
	result.Attempt = n
	result.Start = start
	result.Duration = time.Since(start)

	if runErr != nil {
		result.Status = StatusFailed

		if errors.Is(runErr, errAbandoned) {
			// The operation is still running and the machine state is unknown,
			// so nothing else starts.
			rn.abandoned.CompareAndSwap(nil, r)
		}
	}

	return result, true
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
// success lets dependants run.
func (rn *run) settled(d *Resource) int {
	if !rn.finished(d) {
		return depWaiting
	}

	switch rn.states[d].final.Status {
	case StatusSuccess:
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
