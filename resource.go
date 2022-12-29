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
	Attributes any
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

var allowedKindOperations = map[Operation][]ResourceKind{
	Create: {
		KindApt,
		KindFile,
		KindDirectory,
		KindGit,
		KindLink,
		KindPackage,
	},
	Delete: {
		KindApt,
		KindFile,
		KindDirectory,
		KindGit,
		KindLink,
		KindPackage,
	},
	Run: {
		KindExecute,
	},
	Update: {
		KindApt,
	},
}

// ResourceID is an id of a resource
type ResourceID string

func newResource(o Operation, deps []*Resource) (*Resource, error) {
	var dependsOn []ResourceID
	for _, d := range deps {
		if d.ResourceID == "" {
			return &Resource{}, fmt.Errorf("dependency is not a valid resource for %s", o)
		}
		dependsOn = append(dependsOn, d.ResourceID)
	}

	return &Resource{
		// Operation: o,
		DependsOn: dependsOn,
		Status:    Pending,
	}, nil
}

func (r *Resource) init(a any) error {
	if err := r.setKind(a); err != nil {
		return err
	}

	if r.Attributes.Operation == "" {
		return fmt.Errorf("operation missing")
	}

	if err := r.checkAllowedOperation(); err != nil {
		return err
	}

	return nil
}

func (r *Resource) setKind(a any) error {
	switch a.(type) {
	case Apt:
		r.ResourceKind = KindApt
	case Directory:
		r.ResourceKind = KindDirectory
	case Execute:
		r.ResourceKind = KindExecute
	case File:
		r.ResourceKind = KindFile
	case Git:
		r.ResourceKind = KindGit
	case Link:
		r.ResourceKind = KindLink
	case Package:
		r.ResourceKind = KindPackage
	default:
		return fmt.Errorf("unknown resource kind")
	}

	return nil
}

func (r *Resource) setOperation(a any) error {
	switch r.ResourceKind {
	case KindApt:
		r.Operation = a.(Apt).Operation
	}

}

func (r *Resource) checkAllowedOperation() error {
	if v, ok := allowedKindOperations[r.Operation]; ok {
		for _, k := range v {
			if k == r.ResourceKind {
				return nil
			}
		}
	}

	return fmt.Errorf("operation \"%s\" is not allowed for kind \"%s\"", r.Operation, r.ResourceKind)
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
	r.ResourceID = ResourceID(strings.Join([]string{string(r.ResourceKind), string(r.Operation), idstr}, "_"))
	return nil
}

func (r Resource) run() error {
	switch r.ResourceKind {
	case KindApt:
		attr := r.Attributes.(Apt)

		switch r.Operation {
		case Create:
			log := newLogger("Apt", "create")
			return attr.createApt(log)
		case Delete:
			log := newLogger("Apt", "delete")
			return attr.deleteApt(log)
		case Update:
			log := newLogger("Apt", "update")
			return attr.updateApt(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindFile:
		return r.Attributes.(File).run()
	case KindDirectory:
		attr := r.Attributes.(Directory)

		switch r.Operation {
		case Create:
			log := newLogger("Directory", "create")
			return attr.createDirectory(log)
		case Delete:
			log := newLogger("Directory", "delete")
			return attr.deleteDirectory(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindExecute:
		attr := r.Attributes.(Execute)

		switch r.Operation {
		case Run:
			log := newLogger("Execute", "run")
			return attr.runExecute(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindGit:
		attr := r.Attributes.(Git)

		switch r.Operation {
		case Create:
			log := newLogger("Git", "create")
			return attr.createGit(log)
		case Delete:
			log := newLogger("Git", "delete")
			return attr.deleteGit(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindLink:
		attr := r.Attributes.(Link)

		switch r.Operation {
		case Create:
			log := newLogger("Link", "create")
			return attr.createLink(log)
		case Delete:
			log := newLogger("Link", "delete")
			return attr.deleteLink(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindPackage:
		attr := r.Attributes.(Package)

		switch r.Operation {
		case Create:
			log := newLogger("Package", "create")
			return attr.createPackage(log)
		case Delete:
			log := newLogger("Package", "delete")
			return attr.deletePackage(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	}

	return nil
}
