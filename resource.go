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
	DependsOn []Dependency
	// Status denotes the current status of the resource
	Status
}

// Dependency is an id of a resource
type Dependency string

func DependsOn(in string) Dependency {
	return Dependency(in)
}

func newResource(o Operation, deps []Dependency) *Resource {
	return &Resource{
		Operation: o,
		DependsOn: deps,
		Status:    StatusPending,
	}
}

func (r *Resource) id() string {
	j, err := json.Marshal(r)
	if err != nil {
		log.Fatal(err)
	}

	h := sha1.New()
	h.Write(j)
	return hex.EncodeToString(h.Sum(nil))
}

func (r Resource) run() {
	switch r.Kind {
	case KindFile:
		attr := r.Attributes.(File)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		}
	case KindDirectory:
		attr := r.Attributes.(Directory)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		}
	case KindExecute:
		attr := r.Attributes.(Execute)
		attr.Run()
	case KindGit:
		attr := r.Attributes.(Git)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		}
	case KindLink:
		attr := r.Attributes.(Link)

		switch r.Operation {
		case OperationCreate:
			attr.Create()
		case OperationDelete:
			attr.Delete()
		}
	}
}
