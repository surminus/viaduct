package viaduct

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

type (
	Kind      string
	Operation string
	Status    string
)

const (
	// Operations
	OperationAdd     Operation = "OperationAdd"
	OperationCreate  Operation = "OperationCreate"
	OperationDelete  Operation = "OperationDelete"
	OperationInstall Operation = "OperationInstall"
	OperationRemove  Operation = "OperationRemove"
	OperationRun     Operation = "OperationRun"
	OperationUpdate  Operation = "OperationUpdate"

	// Statuses
	StatusPending Status = "StatusPending"
	StatusSuccess Status = "StatusSuccess"

	// Kinds
	KindApt       Kind = "KindApt"
	KindDirectory Kind = "KindDirectory"
	KindExecute   Kind = "KindExecute"
	KindFile      Kind = "KindFile"
	KindGit       Kind = "KindGit"
	KindLink      Kind = "KindLink"
	KindPackage   Kind = "KindPackage"
)

// Manifest is a map of resources to allow concurrent runs
type Manifest struct {
	resources map[ResourceID]Resource
}

func New() *Manifest {
	return &Manifest{
		resources: make(map[ResourceID]Resource),
	}
}

func (m *Manifest) Create(a any, deps ...ResourceID) (id ResourceID) {
	r := newResource(OperationCreate, deps)

	switch a.(type) {
	case File:
		r.Kind = KindFile
		r.Attributes = a
	case Directory:
		r.Kind = KindDirectory
		r.Attributes = a
	case Git:
		r.Kind = KindGit
		r.Attributes = a
	case Link:
		r.Kind = KindLink
		r.Attributes = a
	default:
		log.Fatal("Operation \"Create\" not supported")
	}

	// Create a string representation of our resource
	r.setID()
	id = r.ResourceID

	if _, ok := m.resources[id]; ok {
		log.Fatal(fmt.Sprintf("Resource already exists with the following attributes:\n%s", r.Attributes))
	}

	m.resources[id] = *r

	return id
}

func (m *Manifest) Delete(a any, deps ...ResourceID) (id ResourceID) {
	r := newResource(OperationDelete, deps)

	switch a.(type) {
	case File:
		r.Kind = KindFile
		r.Attributes = a
	case Directory:
		r.Kind = KindDirectory
		r.Attributes = a
	case Git:
		r.Kind = KindGit
		r.Attributes = a
	case Link:
		r.Kind = KindLink
		r.Attributes = a
	default:
		log.Fatal("Operation \"Delete\" not supported")
	}

	// Create a string representation of our resource
	r.setID()
	id = r.ResourceID

	if _, ok := m.resources[id]; ok {
		log.Fatal(fmt.Sprintf("Resource already exists with the following attributes:\n%s", r.Attributes))
	}

	m.resources[id] = *r

	return id
}

func (m *Manifest) Run(a Execute, deps ...ResourceID) (id ResourceID) {
	r := newResource(OperationRun, deps)
	r.Kind = KindExecute
	r.Attributes = a

	// Create a string representation of our resource
	r.setID()
	id = r.ResourceID

	if _, ok := m.resources[id]; ok {
		log.Fatal(fmt.Sprintf("Resource already exists with the following attributes:\n%s", r.Attributes))
	}

	m.resources[id] = *r

	return id
}

func (m *Manifest) Install(a Package, deps ...ResourceID) (id ResourceID) {
	r := newResource(OperationInstall, deps)

	r.Kind = KindPackage
	r.Attributes = a

	// Create a string representation of our resource
	r.setID()
	id = r.ResourceID

	if _, ok := m.resources[id]; ok {
		log.Fatal(fmt.Sprintf("Resource already exists with the following attributes:\n%s", r.Attributes))
	}

	m.resources[id] = *r

	return id
}

func (m *Manifest) Remove(a any, deps ...ResourceID) (id ResourceID) {
	r := newResource(OperationRemove, deps)

	switch a.(type) {
	case Apt:
		r.Kind = KindApt
		r.Attributes = a
	case Package:
		r.Kind = KindPackage
		r.Attributes = a
	default:
		log.Fatal("Operation \"Remove\" not supported")
	}

	// Create a string representation of our resource
	r.setID()
	id = r.ResourceID

	if _, ok := m.resources[id]; ok {
		log.Fatal(fmt.Sprintf("Resource already exists with the following attributes:\n%s", r.Attributes))
	}

	m.resources[id] = *r

	return id
}

func (m *Manifest) Add(a Apt, deps ...ResourceID) (id ResourceID) {
	r := newResource(OperationAdd, deps)

	r.Kind = KindApt
	r.Attributes = a

	// Create a string representation of our resource
	r.setID()
	id = r.ResourceID

	if _, ok := m.resources[id]; ok {
		log.Fatal(fmt.Sprintf("Resource already exists with the following attributes:\n%s", r.Attributes))
	}

	m.resources[id] = *r

	return id
}

func (m *Manifest) Update(a Apt, deps ...ResourceID) (id ResourceID) {
	r := newResource(OperationUpdate, deps)

	r.Kind = KindApt
	r.Attributes = a

	// Create a string representation of our resource
	r.setID()
	id = r.ResourceID

	if _, ok := m.resources[id]; ok {
		log.Fatal(fmt.Sprintf("Resource already exists with the following attributes:\n%s", r.Attributes))
	}

	m.resources[id] = *r

	return id
}

// Start will run the specified resources concurrently, taking into account
// any dependencies that have been declared
func (m *Manifest) Start() {
	var wg sync.WaitGroup
	var lock sync.RWMutex

	wg.Add(len(m.resources))

	for id, resource := range m.resources {
		go m.runResource(id, resource, &lock, &wg)
	}

	wg.Wait()
}

func (m *Manifest) runResource(id ResourceID, r Resource, lock *sync.RWMutex, wg *sync.WaitGroup) {
	// If we have a dependency, wait until the status of the dependency
	// is successful
	if len(r.DependsOn) > 0 {
		var depsSuccess bool

		// This loop should be tidied
		for i := 0; i < 600000; i++ {
			depsSuccess = true

			lock.RLock()
			for _, dep := range r.DependsOn {
				if d, ok := m.resources[dep]; ok {
					if d.Status != StatusSuccess {
						depsSuccess = false
					}
				}
			}
			lock.RUnlock()

			if depsSuccess {
				break
			}

			// Random sleep to add some jitter
			time.Sleep(time.Duration(rand.Intn(30)) * time.Millisecond)
		}

		if !depsSuccess {
			log.Fatal("Dependency issue: never completed")
		}
	}

	// Run the resource operation
	r.run()

	lock.Lock()
	if re, ok := m.resources[id]; ok {
		re.Status = StatusSuccess
		m.resources[id] = re
	}
	lock.Unlock()

	wg.Done()
}