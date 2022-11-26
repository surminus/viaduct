package viaduct

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log"
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

func newResource(o Operation, deps []ResourceID) *Resource {
	return &Resource{
		Operation: o,
		DependsOn: deps,
		Status:    StatusPending,
	}
}

func (r *Resource) setKind(a any) {
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
		log.Fatalf("Unknown resource kind with attributes %s", a)
	}
}

func (r *Resource) checkAllowedOperation(o Operation) {
	if v, ok := allowedKindOperations[o]; ok {
		for _, k := range v {
			if k == r.ResourceKind {
				return
			}
		}
	}

	log.Fatalf("Operation \"%s\" is not allowed for kind \"%s\"", o, r.ResourceKind)
}

func (r *Resource) setID() {
	j, err := json.Marshal(r)
	if err != nil {
		log.Fatal(err)
	}

	h := sha1.New()
	h.Write(j)
	r.ResourceID = ResourceID(hex.EncodeToString(h.Sum(nil)))
}

func (r Resource) run() {
	switch r.ResourceKind {
	case KindApt:
		attr := r.Attributes.(Apt)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		case OperationUpdate:
			attr.Update()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindFile:
		attr := r.Attributes.(File)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindDirectory:
		attr := r.Attributes.(Directory)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindExecute:
		attr := r.Attributes.(Execute)

		switch r.Operation {
		case OperationRun:
			attr.Run()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindGit:
		attr := r.Attributes.(Git)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindLink:
		attr := r.Attributes.(Link)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	case KindPackage:
		attr := r.Attributes.(Package)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.ResourceKind, r.Operation)
		}
	}
}
