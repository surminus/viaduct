package viaduct

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

const (
	// defaultResourceTimeout is how long a single resource is given to run
	// before the run gives up on it. It bounds one resource's own work, so a
	// manifest can take as long as it likes overall.
	defaultResourceTimeout = 5 * time.Minute
)

// Status is the outcome of a resource in a run.
type Status string

const (
	// StatusPending is a resource that has not finished yet. No Result has it.
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
// concurrently by Apply or Run.
type Manifest struct {
	resources map[ResourceID]*Resource

	// order holds the resources in the order they were added.
	order []*Resource

	// resourceTimeout is the manifest-wide timeout for a single resource.
	// Zero uses defaultResourceTimeout.
	resourceTimeout time.Duration

	// errs holds build errors, which Apply returns before running anything.
	errs []error
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

// SetResourceTimeout sets how long each resource in this manifest may run. The
// default is five minutes.
//
// The timeout applies to each resource, not the whole run. Use WithTimeout to
// raise it for a slow resource.
//
// When it passes, the resource's context is cancelled. A resource that does
// not stop shortly afterwards is abandoned: it is reported as failed, nothing
// else starts, and its operation carries on.
//
// The ResourceTimeout option and the --resource-timeout flag override this,
// and WithTimeout or the resource's ResourceParams override it for a single
// resource. A negative duration means no timeout.
func (m *Manifest) SetResourceTimeout(d time.Duration) {
	m.resourceTimeout = d
}

// WithTimeout sets how long a single resource is given to run, overriding the
// manifest setting and the resource's own. A negative duration means no
// timeout.
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
// when the manifest is applied.
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
// an error that Apply returns. The returned resource can still be used as a
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

		r.Timeout = params.Timeout
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
