package viaduct

import (
	"context"
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

// errAbandoned is returned when a resource is still running once its context
// has been cancelled for a while. The operation carries on, so the run treats
// it as a reason to stop rather than as an ordinary failure.
var errAbandoned = errors.New("abandoned")

// errResourceTimeout is the cause of a resource's context being cancelled by
// its own timeout, rather than by the caller.
var errResourceTimeout = errors.New("resource timeout")

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
	// Timeout overrides how long this resource is given to run. It starts as
	// the Timeout in the resource's params. Zero uses the manifest setting,
	// and a negative duration means no timeout.
	Timeout time.Duration `json:"Timeout,omitempty"`

	// deps and depNames are the dependencies given to Add and SetDep. They are
	// resolved into resolved and DependsOn on apply, so renames do not break
	// them.
	deps     []*Resource
	depNames []ResourceID
	resolved []*Resource

	// invalid marks a resource that could not be added to the manifest.
	invalid bool

	// retry is the retry policy. allowFailure lets the resource fail without
	// failing the run.
	retry        *RetryPolicy
	allowFailure bool
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

	// Timeout is how long the resource is given to run. WithTimeout and the
	// ResourceTimeout option override it. Zero uses the manifest setting, and
	// a negative duration means no timeout.
	Timeout time.Duration
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
	// correctly set, and to ensure that default parameters are assigned. They
	// run for every resource before any resource runs.
	PreflightChecks(ctx context.Context, log *Logger) error

	// Run performs the resource operation. The context carries the Env and is
	// cancelled on timeout or when the run stops. In a dry run, Run skips the
	// changes and logs each one it would have made with Info.
	Run(ctx context.Context, log *Logger) error
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

// abandonGrace is how long a resource has to stop after its context is
// cancelled. It is longer than commandCancelGrace and commandWaitDelay
// together, so a resource whose command stops within its grace is not
// abandoned.
var abandonGrace = 40 * time.Second

// run performs the resource operation, cancelling its context on timeout. A
// timeout of zero or less means no timeout.
//
// A resource that does not return shortly after cancellation is abandoned:
// reported as failed while the operation carries on in the background.
func (r *Resource) run(ctx context.Context, log *Logger, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeoutCause(ctx, timeout, errResourceTimeout)
		defer cancel()
	}

	done := make(chan error, 1)
	go func() {
		done <- r.Attributes.Run(ctx, log)
	}()

	select {
	case err := <-done:
		return stoppedError(ctx, timeout, err)
	case <-ctx.Done():
	}

	select {
	case err := <-done:
		return stoppedError(ctx, timeout, err)
	case <-time.After(abandonGrace):
	}

	if timedOut(ctx) {
		return fmt.Errorf(
			"timed out after %s, and the operation was %w: it may still be running. Raise the limit with WithTimeout, SetResourceTimeout or --resource-timeout",
			timeout,
			errAbandoned,
		)
	}

	return fmt.Errorf("stopped, and the operation was %w: it may still be running: %w", errAbandoned, ctx.Err())
}

// stoppedError is the error for a resource that has returned. Once its
// context is cancelled, returning nil does not count as success, since the
// operation may have been cut short.
func stoppedError(ctx context.Context, timeout time.Duration, err error) error {
	if ctx.Err() == nil {
		return err
	}

	if err == nil {
		err = ctx.Err()
	}

	if timedOut(ctx) {
		return fmt.Errorf("timed out after %s: %w", timeout, err)
	}

	return err
}

// timedOut reports whether the resource's own timeout cancelled its context.
func timedOut(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errResourceTimeout)
}
