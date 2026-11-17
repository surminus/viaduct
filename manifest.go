package viaduct

import (
	"encoding/json"
	"errors"
	"fmt"
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

// Manifest is a map of resources to allow concurrent runs
type Manifest struct {
	resources map[ResourceID]Resource
	collector *ResultCollector

	// resourceTimeout is the manifest-wide timeout for a single resource.
	// Zero uses defaultResourceTimeout.
	resourceTimeout time.Duration

	// abandoned holds the first resource the run gave up on, if any.
	abandoned atomic.Pointer[ResourceID]

	// errs holds build errors, which Run reports before running anything.
	errs []error
}

// New returns an empty manifest.
func New() *Manifest {
	return &Manifest{
		resources: make(map[ResourceID]Resource),
	}
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
// manifest setting.
func (m *Manifest) WithTimeout(r *Resource, d time.Duration) {
	if v, ok := m.resources[r.ResourceID]; ok {
		v.Timeout = d
		m.resources[r.ResourceID] = v
	}
}

// SetName allows us to overwrite the generated ID with our name. This name
// still needs to be unique.
func (m *Manifest) SetName(r *Resource, newName string) {
	if res, ok := m.resources[r.ResourceID]; ok {
		old := r.ResourceID
		newID := ResourceID(newName)

		res.ResourceID = newID
		m.resources[newID] = res

		delete(m.resources, old)
	} else {
		m.errs = append(m.errs, fmt.Errorf("SetName: unknown resource: %s", attrJSON(r.Attributes)))
	}
}

// WithDep sets an explicit dependency using a name
func (m *Manifest) SetDep(r *Resource, name string) {
	if v, ok := m.resources[r.ResourceID]; ok {
		v.DependsOn = append(v.DependsOn, ResourceID(name))
		m.resources[r.ResourceID] = v
	}
}

// WithLock serialises the resource against every other lock holder in the
// run. Use WithLockKey when you know which domain the resource contends on.
func (m *Manifest) WithLock(r *Resource) {
	if v, ok := m.resources[r.ResourceID]; ok {
		v.GlobalLock = true
		// Escalating to the keyless lock has to drop any key the resource
		// already had, or it stays confined to that domain
		v.LockKey = ""
		m.resources[r.ResourceID] = v
	}
}

// WithLockKey takes a lock that only applies to the given domain, such as
// PackageLock or PasswdLock, so the resource only waits for other resources
// using the same key.
func (m *Manifest) WithLockKey(r *Resource, key LockKey) {
	if v, ok := m.resources[r.ResourceID]; ok {
		v.GlobalLock = true
		v.LockKey = key
		m.resources[r.ResourceID] = v
	}
}

func (m *Manifest) addResource(r *Resource, a ResourceAttributes) (err error) {
	// Set attributes
	r.Attributes = a

	if params := a.Params(); params.GlobalLock || params.LockKey != "" {
		r.GlobalLock = true
		r.LockKey = params.LockKey
	}

	// Create a string representation of our resource
	if err := r.setID(); err != nil {
		return err
	}

	if _, ok := m.resources[r.ResourceID]; ok {
		return fmt.Errorf("resource already exists:\n%s", attrJSON(r))
	}

	m.resources[r.ResourceID] = *r

	return err
}

// Add adds a resource to the manifest, to run once every one of deps has
// succeeded. A resource that cannot be added, such as an exact duplicate, is
// recorded as an error that Run reports before running anything.
func (m *Manifest) Add(attributes ResourceAttributes, deps ...*Resource) *Resource {
	r, err := newResource(deps)
	if err == nil {
		err = r.init(attributes)
	}

	if err == nil {
		err = m.addResource(r, attributes)
	}

	if err != nil {
		m.errs = append(m.errs, err)
	}

	return r
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
		if last := chain.Last(); last != nil {
			m.SetDep(to, string(last.ResourceID))
		}
	}

	return chain
}

func attrJSON(a any) string {
	str, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", a)
	}

	return string(str)
}

func (m *Manifest) apply(r Resource, wg *sync.WaitGroup, lock *sync.RWMutex, locks *lockSet) {
	defer wg.Done()

	if err := m.abandonedErr(); err != nil {
		m.fail(&r, lock, StatusDependencyFailed, err)
		return
	}

	if err := m.dependencyCheck(&r, lock); err != nil {
		m.fail(&r, lock, StatusDependencyFailed, err)
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
		m.fail(&r, lock, StatusDependencyFailed, err)
		return
	}

	// Run the resource operation, bounded by its own timeout
	logger, runErr := r.run(m.timeoutFor(&r))
	if runErr != nil {
		if errors.Is(runErr, errAbandoned) {
			// The operation is still going and the machine is in a state we no
			// longer know, so nothing else should start
			id := r.ResourceID
			m.abandoned.CompareAndSwap(nil, &id)
		}

		m.setStatus(&r, lock, StatusFailed)
		m.setError(&r, lock, runErr)
	} else {
		m.setStatus(&r, lock, StatusSuccess)
	}

	if m.collector != nil {
		status := string(StatusSuccess)
		errMsg := ""
		if runErr != nil {
			status = string(StatusFailed)
			errMsg = runErr.Error()
		}

		m.collector.Add(ResourceResult{
			ResourceID:   string(r.ResourceID),
			ResourceKind: string(r.ResourceKind),
			Description:  r.Attributes.Description(),
			Operation:    r.Attributes.OperationName(),
			Status:       status,
			Error:        errMsg,
			Log:          logger.Entries(),
		})
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

// fail records a resource failure, along with its result when collecting for
// JSON output.
func (m *Manifest) fail(r *Resource, lock *sync.RWMutex, status Status, err error) {
	m.setStatus(r, lock, status)
	m.setError(r, lock, err)

	if m.collector != nil {
		m.collector.Add(ResourceResult{
			ResourceID:   string(r.ResourceID),
			ResourceKind: string(r.ResourceKind),
			Description:  r.Attributes.Description(),
			Operation:    r.Attributes.OperationName(),
			Status:       string(status),
			Error:        err.Error(),
		})
	}
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
	if len(r.DependsOn) == 0 {
		return nil
	}

	ticker := time.NewTicker(dependencyPollInterval)
	defer ticker.Stop()

	for {
		depsSuccess := true

		for _, dep := range r.DependsOn {
			lock.RLock()
			d, ok := m.resources[dep]
			lock.RUnlock()

			if !ok {
				continue
			}

			if d.Failed() {
				return fmt.Errorf("upstream dependency %s returned an error", d.ResourceID)
			}

			if d.Status != StatusSuccess {
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

// dependencyCycle returns an error describing a cycle in the dependency graph,
// if there is one. Dependencies on resources that aren't in the manifest are
// ignored, matching how they are skipped during a run.
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
			if _, ok := m.resources[dep]; !ok {
				continue
			}

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

func (m *Manifest) setStatus(r *Resource, lock *sync.RWMutex, s Status) {
	lock.Lock()
	if re, ok := m.resources[r.ResourceID]; ok {
		re.Status = s
		m.resources[r.ResourceID] = re
	}
	lock.Unlock()
}

func (m *Manifest) setError(r *Resource, lock *sync.RWMutex, err error) {
	lock.Lock()
	if re, ok := m.resources[r.ResourceID]; ok {
		re.Error = Error{Err: err, Message: err.Error()}
		m.resources[r.ResourceID] = re
	}
	lock.Unlock()
}
