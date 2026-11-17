package viaduct

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// errInvalidDependency is returned when a dependency was not added to the
// manifest.
var errInvalidDependency = errors.New("dependency is not a valid resource")

// errAbandoned is returned when a resource is still running once its timeout
// has passed. The operation carries on, so the run treats it as a reason to
// stop rather than as an ordinary failure.
var errAbandoned = errors.New("timed out")

// ResourceKind is the kind of resource, such as "File" or "Package".
type ResourceKind string

// Resource holds details about a particular resource during a Viaduct run.
type Resource struct {
	// ResourceID is the resources generated ID.
	ResourceID
	// ResourceKind is what the resource kind is, such as "File" or "Package".
	ResourceKind
	// Attributes are the resource type attributes.
	Attributes ResourceAttributes
	// DependsOn is a list of resource dependencies.
	DependsOn []ResourceID `json:"DependsOn,omitempty"`
	// GlobalLock will mean the resource will not run at the same time
	// as other resources that have this set to true.
	GlobalLock bool
	// LockKey narrows the lock to a single domain, so the resource only
	// waits for other resources using the same key. Empty means the
	// resource is serialised against every other lock holder.
	LockKey LockKey `json:"LockKey,omitempty"`
	// Timeout overrides how long this resource is given to run. Zero uses
	// the manifest setting, and a negative duration means no timeout.
	Timeout time.Duration `json:"Timeout,omitempty"`

	// deps and depNames are the dependencies given to Add and SetDep. They are
	// resolved into resolved and DependsOn on apply, so renames
	// do not break them.
	deps     []*Resource
	depNames []ResourceID
	resolved []*Resource

	// invalid marks a resource that could not be added to the manifest.
	invalid bool
}

// ResourceParams are a set of options that each resource can set
// depending on their logic
type ResourceParams struct {
	// GlobalLock can be set to ensure that the resource uses a global
	// lock during operations
	GlobalLock bool

	// LockKey names the domain the lock applies to, such as PackageLock or
	// PasswdLock. The resource then only waits for other resources using the
	// same key, rather than for every lock holder in the run. Setting a key
	// implies a lock.
	LockKey LockKey
}

// NewResourceParams creates a new ResourceParams.
func NewResourceParams() *ResourceParams {
	return &ResourceParams{}
}

// NewResourceParamsWithLock creates a new ResourceParams, but with
// a global lock applied. The resource is serialised against every other lock
// holder in the run: prefer NewResourceParamsWithLockKey when you know which
// domain the resource contends on.
func NewResourceParamsWithLock() *ResourceParams {
	return &ResourceParams{GlobalLock: true}
}

// NewResourceParamsWithLockKey creates a new ResourceParams with a lock that
// only applies to the given domain, such as PackageLock or PasswdLock.
func NewResourceParamsWithLockKey(key LockKey) *ResourceParams {
	return &ResourceParams{GlobalLock: true, LockKey: key}
}

// ResourceAttributes implement different resource types, such as File or
// Directory. As long as this interface is implemented, then custom resources
// can be directly integrated.
type ResourceAttributes interface {
	// Description returns a short human-readable identifier for this
	// resource instance, such as a file path or command.
	Description() string

	// OperationName is a simple identifier for the operation type, such as
	// Create, Delete, Update or Run.
	OperationName() string

	// Params are a set of parameters that determine how a resource should
	// interact with Viaduct.
	Params() *ResourceParams

	// PreflightChecks are used to check that the resource parameters have been
	// correctly set, and to ensure that default parameters are assigned.
	PreflightChecks(log *Logger) error

	// Run performs the resource operation.
	Run(log *Logger) error
}

// ResourceID is an id of a resource.
type ResourceID string

func (r *Resource) setKind(a ResourceAttributes) error {
	t := reflect.TypeOf(a)
	if t.Kind() != reflect.Pointer {
		return fmt.Errorf("cannot determine resource type")
	}

	r.ResourceKind = ResourceKind(t.Elem().Name())

	return nil
}

// hashID returns an ID made from the resource kind and a hash of the resource.
// Attributes that cannot be marshalled to JSON are hashed by kind alone.
//
// The hashed fields, including a fixed Status and an empty Error, are the
// ones IDs are generated from in every version, so that IDs stay the same.
// Timeout is not among them, so a timeout from the resource's params does not
// change its ID.
func (r *Resource) hashID() ResourceID {
	j, err := json.Marshal(struct {
		ResourceID   ResourceID
		ResourceKind ResourceKind
		Status       Status
		Attributes   ResourceAttributes
		DependsOn    []ResourceID `json:"DependsOn,omitempty"`
		GlobalLock   bool
		LockKey      string `json:"LockKey,omitempty"`
		Error        struct{ Message string }
	}{
		ResourceKind: r.ResourceKind,
		Status:       StatusPending,
		Attributes:   r.Attributes,
		DependsOn:    r.DependsOn,
		GlobalLock:   r.GlobalLock,
		LockKey:      string(r.LockKey),
	})
	if err != nil {
		j = []byte(r.ResourceKind)
	}

	sum := sha1.Sum(j)
	sha := hex.EncodeToString(sum[:])

	return ResourceID(string(r.ResourceKind) + "_id-" + sha[0:8])
}

// name identifies a resource in messages by kind, description and ID.
func (r *Resource) name() string {
	return fmt.Sprintf("%s %s (%s)", r.ResourceKind, r.Attributes.Description(), r.ResourceID)
}

// run performs the resource operation, giving up on it if it takes longer than
// the timeout. A timeout of zero or less lets it run for as long as it takes.
//
// A resource operation cannot be cancelled, since Run takes no context, so an
// abandoned resource carries on in the background: giving up means the run
// stops waiting for it and reports it as failed, not that whatever it was
// doing has stopped.
func (r *Resource) run(log *Logger, timeout time.Duration) error {
	if timeout <= 0 {
		return r.Attributes.Run(log)
	}

	done := make(chan error, 1)
	go func() {
		done <- r.Attributes.Run(log)
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf(
			"%w after %s: the operation was abandoned and may still be running. Raise the limit with WithTimeout, SetResourceTimeout or --resource-timeout",
			errAbandoned,
			timeout,
		)
	}
}
