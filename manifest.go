package viaduct

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultResourceTimeout is how long a single resource is given to run
	// before the run gives up on it. It bounds one resource's own work, so a
	// manifest can take as long as it likes overall.
	defaultResourceTimeout = 5 * time.Minute

	// dependencyPollInterval is how often to check dependency status.
	dependencyPollInterval = 10 * time.Millisecond
)

// Status is the outcome of a resource in a run.
type Status string

const (
	// StatusPending is a resource that has not finished yet.
	StatusPending Status = "Pending"

	// StatusSuccess is a resource that ran without error.
	StatusSuccess Status = "Success"

	// StatusFailed is a resource that returned an error.
	StatusFailed Status = "Failed"

	// StatusDependencyFailed is a resource that did not run because a dependency
	// failed or the run stopped first.
	StatusDependencyFailed Status = "DependencyFailed"
)

// Manifest is a set of resources and the dependencies between them, run
// concurrently by Run.
type Manifest struct {
	resources map[ResourceID]*Resource

	// order holds the resources in the order they were added.
	order []*Resource

	// resourceTimeout is the manifest-wide timeout for a single resource.
	// Zero uses defaultResourceTimeout.
	resourceTimeout time.Duration

	// errs holds build errors, which Run reports before running anything.
	errs []error

	// handler receives the events of the run.
	handler Handler

	// abandoned holds the first resource the run gave up on, if any.
	abandoned atomic.Pointer[ResourceID]
}

// New returns an empty manifest.
func New() *Manifest {
	return &Manifest{resources: make(map[ResourceID]*Resource)}
}

// errorf records an error from building the manifest.
func (m *Manifest) errorf(format string, a ...any) {
	m.errs = append(m.errs, fmt.Errorf(format, a...))
}

// owns reports whether a resource belongs to this manifest. A resource that
// could not be added counts as owned, as its error is already recorded.
func (m *Manifest) owns(r *Resource) bool {
	if r == nil {
		return false
	}

	return r.invalid || m.resources[r.ResourceID] == r
}

// lookup returns the manifest's copy of a resource, recording an error naming
// the method if it is not in the manifest.
func (m *Manifest) lookup(method string, r *Resource) (*Resource, bool) {
	if r == nil {
		m.errorf("%s: resource is nil", method)
		return nil, false
	}

	if r.invalid {
		return nil, false
	}

	if !m.owns(r) {
		m.errorf("%s: %s is not in this manifest", method, r.ResourceID)
		return nil, false
	}

	return r, true
}

// SetResourceTimeout sets how long each resource in this manifest is given to
// run before the run gives up on it. The default is five minutes.
//
// The timeout applies to a single resource rather than to the run, so a manifest
// can take as long as it likes so long as no individual resource overruns. Raise
// it for a resource that is legitimately slow, such as a large download or a
// clone of a big repository, with WithTimeout.
//
// Be aware that a resource cannot be cancelled, so a timeout stops the run
// waiting for it and reports it as failed while the operation itself carries on:
// whatever it was part way through, such as a download or a package install, is
// left as it is.
//
// The --resource-timeout flag overrides this, and WithTimeout overrides it for
// a single resource. A negative duration means no timeout.
func (m *Manifest) SetResourceTimeout(d time.Duration) {
	m.resourceTimeout = d
}

// WithTimeout sets how long a single resource is given to run, overriding the
// manifest setting. A negative duration means no timeout.
func (m *Manifest) WithTimeout(r *Resource, d time.Duration) {
	if r, ok := m.lookup("WithTimeout", r); ok {
		r.Timeout = d
	}
}

// SetName replaces a resource's generated ID with a name, which must be unique
// within the manifest.
func (m *Manifest) SetName(r *Resource, name string) {
	r, ok := m.lookup("SetName", r)
	if !ok {
		return
	}

	id := ResourceID(name)
	if id == r.ResourceID {
		return
	}

	if err := m.claim(id); err != nil {
		m.errs = append(m.errs, err)
		return
	}

	delete(m.resources, r.ResourceID)
	r.ResourceID = id
	m.resources[id] = r
}

// claim checks that a name is free to use as an ID.
func (m *Manifest) claim(id ResourceID) error {
	if id == "" {
		return errors.New("a resource name cannot be empty")
	}

	if _, ok := m.resources[id]; ok {
		return fmt.Errorf("a resource named %s already exists", id)
	}

	return nil
}

// SetDep makes a resource depend on another by ID or name. The name must exist
// when the manifest is run.
func (m *Manifest) SetDep(r *Resource, name string) {
	if r, ok := m.lookup("SetDep", r); ok {
		r.depNames = append(r.depNames, ResourceID(name))
	}
}

// WithLock serialises the resource against every other lock holder in the
// run. Use WithLockKey when you know which domain the resource contends on.
func (m *Manifest) WithLock(r *Resource) {
	if r, ok := m.lookup("WithLock", r); ok {
		r.GlobalLock = true
		// Drop any existing key, or the lock stays confined to that domain
		r.LockKey = ""
	}
}

// WithLockKey takes a lock that only applies to the given domain, such as
// PackageLock or PasswdLock, so the resource only waits for other resources
// using the same key.
func (m *Manifest) WithLockKey(r *Resource, key LockKey) {
	if r, ok := m.lookup("WithLockKey", r); ok {
		r.GlobalLock = true
		r.LockKey = key
	}
}

// Add adds a resource to the manifest, to run once every one of deps has
// succeeded. Its ID is generated from its kind and attributes.
//
// A resource that cannot be added, such as an exact duplicate, is recorded as
// an error that Run reports. The returned resource can still be used as a
// dependency.
func (m *Manifest) Add(attributes ResourceAttributes, deps ...*Resource) *Resource {
	return m.add("", attributes, deps)
}

// AddNamed is like Add, but uses the name as the resource's ID. The name has to
// be unique within the manifest.
func (m *Manifest) AddNamed(name string, attributes ResourceAttributes, deps ...*Resource) *Resource {
	if name == "" {
		m.errorf("AddNamed: a resource name cannot be empty")
		return &Resource{Attributes: attributes, invalid: true}
	}

	return m.add(ResourceID(name), attributes, deps)
}

func (m *Manifest) add(name ResourceID, attributes ResourceAttributes, deps []*Resource) *Resource {
	r, err := m.newResource(attributes, deps)
	if err == nil {
		err = m.insert(r, name)
	}

	if err != nil {
		r.invalid = true

		// Already reported when it was added
		if !errors.Is(err, errInvalidDependency) {
			m.errs = append(m.errs, err)
		}
	}

	return r
}

// newResource builds a resource from its attributes, without adding it.
func (m *Manifest) newResource(a ResourceAttributes, deps []*Resource) (*Resource, error) {
	r := &Resource{Attributes: a}

	if a == nil {
		return r, errors.New("resource attributes are nil")
	}

	for _, d := range deps {
		switch {
		case d == nil:
			return r, fmt.Errorf("%s depends on a nil resource", a.Description())
		case d.invalid:
			return r, errInvalidDependency
		case !m.owns(d):
			return r, fmt.Errorf("%s depends on %s, which is not in this manifest", a.Description(), d.ResourceID)
		}

		r.deps = append(r.deps, d)
		r.DependsOn = append(r.DependsOn, d.ResourceID)
	}

	if err := r.setKind(a); err != nil {
		return r, err
	}

	if params := a.Params(); params != nil {
		if params.GlobalLock || params.LockKey != "" {
			r.GlobalLock = true
			r.LockKey = params.LockKey
		}

	}

	return r, nil
}

// insert adds a built resource under its name, or a generated ID if unnamed.
func (m *Manifest) insert(r *Resource, name ResourceID) error {
	if name != "" {
		if err := m.claim(name); err != nil {
			return err
		}

		r.ResourceID = name
	} else {
		id, err := m.generateID(r)
		if err != nil {
			return err
		}

		r.ResourceID = id
	}

	m.resources[r.ResourceID] = r
	m.order = append(m.order, r)

	return nil
}

// generateID returns an ID from a hash of the resource's kind and attributes.
// An identical resource is an error. One that differs only in attributes JSON
// cannot represent gets a numbered suffix in order of addition.
func (m *Manifest) generateID(r *Resource) (ResourceID, error) {
	base := r.hashID()

	existing, ok := m.resources[base]
	if !ok {
		return base, nil
	}

	if reflect.DeepEqual(existing.Attributes, r.Attributes) && slices.Equal(existing.DependsOn, r.DependsOn) {
		return "", fmt.Errorf("%s %s is already in the manifest; use AddNamed to add it under another name", r.ResourceKind, r.Attributes.Description())
	}

	for n := 2; ; n++ {
		id := ResourceID(fmt.Sprintf("%s-%d", base, n))
		if _, ok := m.resources[id]; !ok {
			return id, nil
		}
	}
}

// ResourceChain is the ordered sequence of resources returned by Chain,
// ChainFrom and ChainTo. Within the chain each resource depends on the one
// before it; the first resource additionally depends on the resource passed
// to ChainFrom, if any.
type ResourceChain []*Resource

// Last returns the final resource in the chain, or nil if the chain is empty.
// It's the usual thing to depend on when wiring more work after a chain.
func (c ResourceChain) Last() *Resource {
	if len(c) == 0 {
		return nil
	}

	return c[len(c)-1]
}

// First returns the first resource in the chain, or nil if the chain is empty.
func (c ResourceChain) First() *Resource {
	if len(c) == 0 {
		return nil
	}

	return c[0]
}

// Chain adds a sequence of resources where each one depends on the resource
// before it, wiring a -> b -> c in a single call. It returns the created
// resources in order, so you can still branch off any individual link.
//
// To start a chain from a resource that already exists, use ChainFrom.
func (m *Manifest) Chain(attributes ...ResourceAttributes) ResourceChain {
	return m.ChainFrom(nil, attributes...)
}

// ChainFrom is like Chain, but the first resource in the chain depends on an
// existing resource. from may be nil, in which case the chain has no initial
// dependency and ChainFrom behaves exactly like Chain.
func (m *Manifest) ChainFrom(from *Resource, attributes ...ResourceAttributes) ResourceChain {
	resources := make(ResourceChain, 0, len(attributes))

	prev := from
	for _, a := range attributes {
		var r *Resource
		if prev == nil {
			r = m.Add(a)
		} else {
			r = m.Add(a, prev)
		}

		resources = append(resources, r)
		prev = r
	}

	return resources
}

// ChainTo is like Chain, but an existing resource is made to depend on the
// last resource in the chain, so it runs only once the whole chain has
// completed. to may be nil, in which case ChainTo behaves exactly like Chain.
//
// The returned chain does not include to, mirroring how ChainFrom does not
// include the resource it starts from.
func (m *Manifest) ChainTo(to *Resource, attributes ...ResourceAttributes) ResourceChain {
	chain := m.Chain(attributes...)

	if to != nil {
		if last := chain.Last(); last != nil && !last.invalid {
			if to, ok := m.lookup("ChainTo", to); ok {
				to.deps = append(to.deps, last)
			}
		}
	}

	return chain
}

// compile resolves every dependency to a resource in the manifest, and checks
// that they can all be satisfied.
func (m *Manifest) compile() error {
	var errs []error

	for _, r := range m.order {
		r.DependsOn = nil
		r.resolved = nil

		seen := map[*Resource]bool{}

		add := func(d *Resource) {
			if !seen[d] {
				seen[d] = true
				r.resolved = append(r.resolved, d)
				r.DependsOn = append(r.DependsOn, d.ResourceID)
			}
		}

		for _, d := range r.deps {
			add(d)
		}

		for _, name := range r.depNames {
			d, ok := m.resources[name]
			if !ok {
				errs = append(errs, fmt.Errorf("%s depends on %s, which is not in this manifest", r.ResourceID, name))
				continue
			}

			add(d)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return err
	}

	return m.dependencyCycle()
}

// dependencyCycle returns an error describing a cycle in the dependency graph,
// if there is one.
func (m *Manifest) dependencyCycle() error {
	const (
		unvisited = iota
		visiting
		visited
	)

	state := make(map[ResourceID]int, len(m.resources))
	var path []ResourceID

	var walk func(id ResourceID) []ResourceID
	walk = func(id ResourceID) []ResourceID {
		state[id] = visiting
		path = append(path, id)

		for _, dep := range sortedIDs(m.resources[id].DependsOn) {
			switch state[dep] {
			case visiting:
				// Trim the path back to where the cycle starts, and close it.
				for i, p := range path {
					if p == dep {
						return append(append([]ResourceID(nil), path[i:]...), dep)
					}
				}
			case unvisited:
				if cycle := walk(dep); cycle != nil {
					return cycle
				}
			}
		}

		path = path[:len(path)-1]
		state[id] = visited

		return nil
	}

	ids := make([]ResourceID, 0, len(m.resources))
	for id := range m.resources {
		ids = append(ids, id)
	}

	// Walk in a stable order so the reported cycle doesn't change between runs.
	for _, id := range sortedIDs(ids) {
		if state[id] != unvisited {
			continue
		}

		if cycle := walk(id); cycle != nil {
			names := make([]string, 0, len(cycle))
			for _, id := range cycle {
				names = append(names, string(id))
			}

			return fmt.Errorf("dependency cycle detected: %s", strings.Join(names, " -> "))
		}
	}

	return nil
}

func sortedIDs(ids []ResourceID) []ResourceID {
	out := make([]ResourceID, len(ids))
	copy(out, ids)
	slices.Sort(out)

	return out
}

func (m *Manifest) apply(r *Resource, wg *sync.WaitGroup, lock *sync.RWMutex, locks *lockSet) {
	defer wg.Done()

	if err := m.abandonedErr(); err != nil {
		m.fail(r, lock, StatusDependencyFailed, err)
		return
	}

	if err := m.dependencyCheck(r, lock); err != nil {
		m.fail(r, lock, StatusDependencyFailed, err)
		return
	}

	if r.GlobalLock {
		release := locks.acquire(r.LockKey)
		defer release()
	}

	// The lock is released when a resource is abandoned, because holding it
	// would block everything sharing the key for the rest of the run. That
	// means whatever was waiting for it has to check again: an abandoned
	// operation still holds the real lock, dpkg's or passwd's, so starting the
	// next one now would just fail against it.
	if err := m.abandonedErr(); err != nil {
		m.fail(r, lock, StatusDependencyFailed, err)
		return
	}

	// Run the resource operation, bounded by its own timeout
	runErr := r.run(m.handler, m.timeoutFor(r))
	if runErr != nil {
		if errors.Is(runErr, errAbandoned) {
			// The operation is still going and the machine is in a state we no
			// longer know, so nothing else should start
			id := r.ResourceID
			m.abandoned.CompareAndSwap(nil, &id)
		}

		m.setStatus(r, lock, StatusFailed)
		m.setError(r, lock, runErr)
	} else {
		m.setStatus(r, lock, StatusSuccess)
	}
}

// abandonedErr reports why nothing further should start, once the run has given
// up on a resource that is still running.
func (m *Manifest) abandonedErr() error {
	if id := m.abandoned.Load(); id != nil {
		return fmt.Errorf("not started: the run gave up on %s, which may still be running", *id)
	}

	return nil
}

// fail records a resource failure.
func (m *Manifest) fail(r *Resource, lock *sync.RWMutex, status Status, err error) {
	m.setStatus(r, lock, status)
	m.setError(r, lock, err)
}

// dependencyCheck blocks until every dependency of the resource has succeeded,
// returning an error if one of them failed.
//
// There is no timeout on the wait itself, because waiting is not what goes
// wrong: every resource is bounded by its own timeout, and dependency cycles
// are rejected before the run starts, so a dependency always reaches a
// terminal status eventually. Timing out here would only fail whichever
// resource happened to be waiting on the slow one, which is the least useful
// place to report it.
func (m *Manifest) dependencyCheck(r *Resource, lock *sync.RWMutex) error {
	if len(r.resolved) == 0 {
		return nil
	}

	ticker := time.NewTicker(dependencyPollInterval)
	defer ticker.Stop()

	for {
		depsSuccess := true

		for _, d := range r.resolved {
			lock.RLock()
			status := d.Status
			lock.RUnlock()

			if status == StatusFailed || status == StatusDependencyFailed {
				return fmt.Errorf("upstream dependency %s returned an error", d.ResourceID)
			}

			if status != StatusSuccess {
				depsSuccess = false
			}
		}

		if depsSuccess {
			return nil
		}

		<-ticker.C
	}
}

// timeoutFor returns how long a resource is given to run. The
// --resource-timeout flag wins, so a slow run can be rescued without
// recompiling, then the resource, then the manifest, then the default. A
// negative duration means no timeout.
func (m *Manifest) timeoutFor(r *Resource) time.Duration {
	switch {
	case Cli.ResourceTimeout != 0:
		return Cli.ResourceTimeout
	case r.Timeout != 0:
		return r.Timeout
	case m.resourceTimeout != 0:
		return m.resourceTimeout
	default:
		return defaultResourceTimeout
	}
}

func (m *Manifest) setStatus(r *Resource, lock *sync.RWMutex, s Status) {
	lock.Lock()
	r.Status = s
	lock.Unlock()
}

func (m *Manifest) setError(r *Resource, lock *sync.RWMutex, err error) {
	lock.Lock()
	r.Error = Error{Err: err, Message: err.Error()}
	lock.Unlock()
}
