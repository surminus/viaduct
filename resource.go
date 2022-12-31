package viaduct

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// Resource Kinds
	KindApt       ResourceKind = "Apt"
	KindDirectory ResourceKind = "Directory"
	KindExecute   ResourceKind = "Execute"
	KindFile      ResourceKind = "File"
	KindGit       ResourceKind = "Git"
	KindLink      ResourceKind = "Link"
	KindPackage   ResourceKind = "Package"
)

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

// ResourceAttributes implement resource types.
type ResourceAttributes interface {
	run(log *logger) error
	satisfy(log *logger) error
	operationName() string
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
	switch a.(type) {
	case *Apt:
		r.ResourceKind = KindApt
	case *Directory:
		r.ResourceKind = KindDirectory
	case *Execute:
		r.ResourceKind = KindExecute
	case *File:
		r.ResourceKind = KindFile
	case *Git:
		r.ResourceKind = KindGit
	case *Link:
		r.ResourceKind = KindLink
	case *Package:
		r.ResourceKind = KindPackage
	default:
		return fmt.Errorf("unknown resource kind")
	}

	return nil
}

func (r *Resource) setID() error {
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
