package viaduct

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log"
)

// Resource specifies how a new resource should be run
type Resource struct {
	Attributes any
	// The kind of resource, such as "file" or "package"
	Kind
	// What the resource is doing, such as "create" or "delete"
	Operation
	// A list of resource dependencies
	DependsOn []ResourceID
	// Status denotes the current status of the resource
	Status
	// ResourceID is the resources generated ID
	ResourceID
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
	switch r.Kind {
	case KindApt:
		attr := r.Attributes.(Apt)

		switch r.Operation {
		case OperationAdd:
			attr.Add()
		case OperationRemove:
			attr.Remove()
		case OperationUpdate:
			attr.Update()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.Kind, r.Operation)
		}
	case KindFile:
		attr := r.Attributes.(File)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.Kind, r.Operation)
		}
	case KindDirectory:
		attr := r.Attributes.(Directory)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.Kind, r.Operation)
		}
	case KindExecute:
		attr := r.Attributes.(Execute)

		switch r.Operation {
		case OperationRun:
			attr.Run()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.Kind, r.Operation)
		}
	case KindGit:
		attr := r.Attributes.(Git)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.Kind, r.Operation)
		}
	case KindLink:
		attr := r.Attributes.(Link)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.Kind, r.Operation)
		}
	case KindPackage:
		attr := r.Attributes.(Package)

		switch r.Operation {
		case OperationInstall:
			attr.Install()
		case OperationRemove:
			attr.Remove()
		default:
			log.Fatalf("Unknown operation for %s: %s", r.Kind, r.Operation)
		}
	}
}
