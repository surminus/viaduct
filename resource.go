package viaduct

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	// Resource Kinds
	KindApt       ResourceKind = "KindApt"
	KindDirectory ResourceKind = "KindDirectory"
	KindExecute   ResourceKind = "KindExecute"
	KindFile      ResourceKind = "KindFile"
	KindGit       ResourceKind = "KindGit"
	KindLink      ResourceKind = "KindLink"
	KindPackage   ResourceKind = "KindPackage"
)

// Resource specifies how a new resource should be run
type Resource struct {
	Attributes any
	// The kind of resource, such as "file" or "package"
	ResourceKind
	// What the resource is doing, such as "create" or "delete"
	Operation
	// A list of resource dependencies
	DependsOn []ResourceID
	// Status denotes the current status of the resource
	Status
	// ResourceID is the resources generated ID
	ResourceID
	// GlobalLock will mean the resource will not run at the same time
	// as other resources that have this set to true
	GlobalLock bool
}

var allowedKindOperations = map[Operation][]ResourceKind{
	OperationCreate: {
		KindApt,
		KindFile,
		KindDirectory,
		KindGit,
		KindLink,
		KindPackage,
	},
	OperationDelete: {
		KindApt,
		KindFile,
		KindDirectory,
		KindGit,
		KindLink,
		KindPackage,
	},
	OperationRun: {
		KindExecute,
	},
	OperationUpdate: {
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
		Operation: o,
		DependsOn: dependsOn,
		Status:    StatusPending,
	}, nil
}

func (r *Resource) init(a any) error {
	if err := r.setKind(a); err != nil {
		return err
	}

	if r.Operation == "" {
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
	r.ResourceID = ResourceID(hex.EncodeToString(h.Sum(nil)))
	return nil
}

func (r Resource) run() error {
	switch r.ResourceKind {
	case KindApt:
		attr := r.Attributes.(Apt)

		switch r.Operation {
		case OperationCreate:
			log := newLogger("Apt", "create")
			return attr.createApt(log)
		case OperationDelete:
			log := newLogger("Apt", "delete")
			return attr.deleteApt(log)
		case OperationUpdate:
			log := newLogger("Apt", "update")
			return attr.updateApt(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindFile:
		attr := r.Attributes.(File)

		switch r.Operation {
		case OperationCreate:
			log := newLogger("File", "create")
			return attr.createFile(log)
		case OperationDelete:
			log := newLogger("File", "delete")
			return attr.deleteFile(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindDirectory:
		attr := r.Attributes.(Directory)

		switch r.Operation {
		case OperationCreate:
			log := newLogger("Directory", "create")
			return attr.createDirectory(log)
		case OperationDelete:
			log := newLogger("Directory", "delete")
			return attr.deleteDirectory(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindExecute:
		attr := r.Attributes.(Execute)

		switch r.Operation {
		case OperationRun:
			log := newLogger("Execute", "run")
			return attr.runExecute(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindGit:
		attr := r.Attributes.(Git)

		switch r.Operation {
		case OperationCreate:
			log := newLogger("Git", "create")
			return attr.createGit(log)
		case OperationDelete:
			log := newLogger("Git", "delete")
			return attr.deleteGit(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindLink:
		attr := r.Attributes.(Link)

		switch r.Operation {
		case OperationCreate:
			log := newLogger("Link", "create")
			return attr.createLink(log)
		case OperationDelete:
			log := newLogger("Link", "delete")
			return attr.deleteLink(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindPackage:
		attr := r.Attributes.(Package)

		switch r.Operation {
		case OperationCreate:
			log := newLogger("Package", "create")
			return attr.createPackage(log)
		case OperationDelete:
			log := newLogger("Package", "delete")
			return attr.deletePackage(log)
		default:
			return fmt.Errorf("unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	}

	return nil
}
