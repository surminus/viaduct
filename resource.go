package viaduct

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type ResourceKind string

// Resource specifies how a new resource should be run
type Resource struct {
	Attributes ResourceAttributes
	// The kind of resource, such as "file" or "package"
	ResourceKind
	// What the resource is doing, such as "create" or "delete"
	// Operation
	// A list of resource dependencies
	DependsOn []ResourceID
	// Status denotes the current status of the resource
	Status
	// ResourceID is the resources generated ID
	ResourceID
	// GlobalLock will mean the resource will not run at the same time
	// as other resources that have this set to true
	GlobalLock bool
	// Error contains any errors raised during a run
	Error string
}

// ResourceOptions are a set of options that each resource can set
// depending on their logic
type ResourceOptions struct {
	// GlobalLock can be set to ensure that the resource uses a global
	// lock during operations
	GlobalLock bool
}

func NewResourceOptions() *ResourceOptions {
	return &ResourceOptions{}
}

func NewResourceOptionsWithLock() *ResourceOptions {
	return &ResourceOptions{GlobalLock: true}
}

// ResourceAttributes implement resource types.
type ResourceAttributes interface {
	operationName() string
	opts() *ResourceOptions
	run(log *logger) error
	satisfy(log *logger) error
}

// ResourceID is an id of a resource
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

func (r *Resource) run() error {
	log := newLogger(string(r.ResourceKind), r.Attributes.operationName())

	if err := r.Attributes.satisfy(log); err != nil {
		return err
	}

	return r.Attributes.run(log)
}
