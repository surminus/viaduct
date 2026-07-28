package viaduct

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

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
	// Status denotes the current status of the resource.
	Status
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
	LockKey string `json:"LockKey,omitempty"`
	// Timeout overrides how long this resource is given to run. Zero uses
	// the manifest setting, and a negative duration means no timeout.
	Timeout time.Duration `json:"Timeout,omitempty"`
	// Error contains any errors raised during a run.
	Error `json:"Error"`
}

type Error struct {
	Err     error  `json:"-"`
	Message string `json:"Message"`
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
	LockKey string
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
func NewResourceParamsWithLockKey(key string) *ResourceParams {
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

func newResource(deps []*Resource) (*Resource, error) {
	var dependsOn []ResourceID
	for _, d := range deps {
		if d.ResourceID == "" {
			return &Resource{}, fmt.Errorf("dependency is not a valid resource: %s", attrJSON(d))
		}
		dependsOn = append(dependsOn, d.ResourceID)
	}

	return &Resource{
		DependsOn: dependsOn,
		Status:    Pending,
	}, nil
}

func (r *Resource) init(a ResourceAttributes) error {
	if err := r.setKind(a); err != nil {
		return err
	}

	return nil
}

func (r *Resource) setKind(a ResourceAttributes) error {
	t := reflect.TypeOf(a)
	if t.Kind() != reflect.Pointer {
		return fmt.Errorf("cannot determine resource type")
	}

	k := t.Elem().Name()

	r.ResourceKind = ResourceKind(k)

	return nil
}

func (r *Resource) setID() error {
	if r.ResourceKind == "" {
		return fmt.Errorf("resource kind has not been set")
	}

	j, err := json.Marshal(r)
	if err != nil {
		return err
	}

	h := sha1.New()
	h.Write(j)
	sha := hex.EncodeToString(h.Sum(nil))

	idstr := strings.Join([]string{"id", sha[0:8]}, "-")
	r.ResourceID = ResourceID(strings.Join([]string{string(r.ResourceKind), idstr}, "_"))
	return nil
}

func (r *Resource) preflight() error {
	log := NewLogger(string(r.ResourceKind), "Preflight")
	return r.Attributes.PreflightChecks(log)
}

// run performs the resource operation, giving up on it if it takes longer than
// the timeout. A timeout of zero or less lets it run for as long as it takes.
//
// A resource operation cannot be cancelled, since Run takes no context, so an
// abandoned resource carries on in the background: giving up means the run
// stops waiting for it and reports it as failed, not that whatever it was
// doing has stopped.
func (r *Resource) run(timeout time.Duration) (*Logger, error) {
	log := NewLogger(string(r.ResourceKind), r.Attributes.OperationName())

	if timeout <= 0 {
		return log, r.Attributes.Run(log)
	}

	done := make(chan error, 1)
	go func() {
		done <- r.Attributes.Run(log)
	}()

	select {
	case err := <-done:
		return log, err
	case <-time.After(timeout):
		return log, fmt.Errorf(
			"%w after %s: the operation was abandoned and may still be running. Raise the limit with WithTimeout, SetResourceTimeout or --resource-timeout",
			errAbandoned,
			timeout,
		)
	}
}

func (r *Resource) Failed() bool {
	return r.Status == Failed || r.Status == DependencyFailed
}
