package viaduct

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"
)

type (
	ResourceKind string
	Operation    string
	Status       string
)

const (
	// Operations
	OperationCreate Operation = "OperationCreate"
	OperationDelete Operation = "OperationDelete"
	OperationRun    Operation = "OperationRun"
	OperationUpdate Operation = "OperationUpdate"

	// Statuses
	StatusDependencyFailed Status = "StatusDependencyFailed"
	StatusFailed           Status = "StatusFailed"
	StatusPending          Status = "StatusPending"
	StatusSuccess          Status = "StatusSuccess"
)

// Manifest is a map of resources to allow concurrent runs
type Manifest struct {
	resources map[ResourceID]Resource
	errors    chan error
}

func New() *Manifest {
	return &Manifest{
		resources: make(map[ResourceID]Resource),
	}
}

// SetName allows us to overwrite the generated ID with our name. This name
// still needs to be unique.
func (m *Manifest) SetName(r *Resource, newName string) {
	if res, ok := m.resources[r.ResourceID]; ok {
		old := r.ResourceID
		newID := ResourceID(newName)

		res.ResourceID = newID
		m.resources[newID] = res

		delete(m.resources, old)
	} else {
		log.Fatalf("Unknown resource: %s", attrJSON(r.Attributes))
	}
}

// WithDep sets an explicit dependency using a name
func (m *Manifest) SetDep(r *Resource, name string) {
	if v, ok := m.resources[r.ResourceID]; ok {
		v.DependsOn = append(v.DependsOn, ResourceID(name))
		m.resources[r.ResourceID] = v
	}
}

func (m *Manifest) WithLock(r *Resource) {
	if v, ok := m.resources[r.ResourceID]; ok {
		v.GlobalLock = true
		m.resources[r.ResourceID] = v
	}
}

func (m *Manifest) addResource(r *Resource, a any) (err error) {
	// Set attributes
	r.Attributes = a

	// Package resources should never run at the same time, and
	// the AptUpdate operation should also take a global lock.
	if r.ResourceKind == KindPackage || r.Operation == OperationUpdate {
		r.GlobalLock = true
	}

	// Create a string representation of our resource
	if err := r.setID(); err != nil {
		return err
	}

	if _, ok := m.resources[r.ResourceID]; ok {
		return fmt.Errorf("resource already exists with attributes: %s", a)
	}

	m.resources[r.ResourceID] = *r

	return err
}

func attrJSON(a any) string {
	str, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	return string(str)
}

// createResource is internally used for testing
func (m *Manifest) createResource(a any, deps ...*Resource) (*Resource, error) {
	r, err := newResource(OperationCreate, deps)
	if err != nil {
		return r, err
	}

	if err := r.init(a); err != nil {
		return r, err
	}

	err = m.addResource(r, a)
	return r, err
}

// Create adds a creation resource to the manifest
func (m *Manifest) Create(a any, deps ...*Resource) *Resource {
	log := newLogger("Viaduct", "create")

	r, err := m.createResource(a, deps...)
	if err != nil {
		log.Fatal(err)
	}

	return r
}

// deleteResource is internally used for testing
func (m *Manifest) deleteResource(a any, deps ...*Resource) (*Resource, error) {
	r, err := newResource(OperationDelete, deps)
	if err != nil {
		return r, err
	}

	if err := r.init(a); err != nil {
		return r, err
	}

	err = m.addResource(r, a)
	return r, err
}

// Delete adds a deletion resource to the manifest
func (m *Manifest) Delete(a any, deps ...*Resource) *Resource {
	log := newLogger("Viaduct", "delete")

	r, err := m.deleteResource(a, deps...)
	if err != nil {
		log.Fatal(err)
	}

	return r
}

// runResource is internally used for testing
func (m *Manifest) runResource(a Execute, deps ...*Resource) (*Resource, error) {
	r, err := newResource(OperationRun, deps)
	if err != nil {
		return r, err
	}

	if err := r.init(a); err != nil {
		return r, err
	}

	err = m.addResource(r, a)
	return r, err
}

// Run adds an execution run to the manifest
func (m *Manifest) Run(a Execute, deps ...*Resource) *Resource {
	log := newLogger("Viaduct", "run")

	r, err := m.runResource(a, deps...)
	if err != nil {
		log.Fatal(err)
	}

	return r
}

// updateResource is used internally for testing
func (m *Manifest) updateResource(a Apt, deps ...*Resource) (*Resource, error) {
	r, err := newResource(OperationUpdate, deps)
	if err != nil {
		return r, err
	}

	if err := r.init(a); err != nil {
		return r, err
	}

	err = m.addResource(r, a)
	return r, err
}

func (m *Manifest) Update(a Apt, deps ...*Resource) *Resource {
	log := newLogger("Viaduct", "update")

	r, err := m.updateResource(a, deps...)
	if err != nil {
		log.Fatal(err)
	}

	return r
}

// Start will run the specified resources concurrently, taking into account
// any dependencies that have been declared
func (m *Manifest) Start() {
	log := newLogger("Viaduct", "run")
	start := time.Now()
	log.Info("Start")

	// var g errgroup.Group
	var lock, globalLock sync.RWMutex
	var wg sync.WaitGroup
	m.errors = make(chan error, len(m.resources))

	wg.Add(len(m.resources))

	for id, resource := range m.resources {
		go m.apply(id, resource, &wg, &lock, &globalLock)
	}

	wg.Wait()
	close(m.errors)

	if len(m.errors) > 0 {
		timeTaken := time.Since(start).Round(time.Second).String()
		log.Warn(fmt.Sprintf("Completed with errors in %s:", timeTaken))

		tmpName := fmt.Sprintf("/tmp/viaduct-%d.json", time.Now().Unix())

		out, err := json.MarshalIndent(m.resources, "", "    ")
		if err != nil {
			log.Fatal(err)
		}

		err = os.WriteFile(tmpName, out, 0o644)
		if err != nil {
			log.Fatal(err)
		}

		log.Warn(fmt.Sprintf("Manifest written to: %s", tmpName))
	} else {
		timeTaken := time.Since(start).Round(time.Second).String()
		log.Info(fmt.Sprintf("Completed without errors in %s", timeTaken))
	}
}

func (m *Manifest) apply(id ResourceID, r Resource, wg *sync.WaitGroup, lock *sync.RWMutex, globalLock *sync.RWMutex) {
	defer wg.Done()

	err := m.dependencyCheck(&r, lock)
	if err != nil {
		m.errors <- err
		return
	}

	if r.GlobalLock {
		globalLock.Lock()
	}

	// Run the resource operation
	err = r.run()
	if err != nil {
		m.setStatus(&r, StatusFailed, lock)
		m.errors <- err
		return
	}

	m.setStatus(&r, StatusSuccess, lock)

	if r.GlobalLock {
		globalLock.Unlock()
	}
}

func (m *Manifest) dependencyCheck(r *Resource, lock *sync.RWMutex) error {
	// If we have a dependency, wait until the status of the dependency
	// is successful
	if len(r.DependsOn) > 0 {
		var depsSuccess bool

		// This loop should be tidied
		for i := 0; i < 600000; i++ {
			depsSuccess = true

			for _, dep := range r.DependsOn {
				lock.RLock()
				if d, ok := m.resources[dep]; ok {
					if d.Status == StatusFailed {
						lock.RUnlock()
						m.setStatus(r, StatusDependencyFailed, lock)

						// Unlock and return error
						return fmt.Errorf("dependency failed, refusing to run")
					}

					if d.Status != StatusSuccess {
						depsSuccess = false
					}
				}
				lock.RUnlock()
			}

			if depsSuccess {
				break
			}

			// Random sleep to add some jitter
			time.Sleep(time.Duration(rand.Intn(30)) * time.Millisecond)
		}

		if !depsSuccess {
			return fmt.Errorf("resource %s gave up waiting for dependencies", string(r.ResourceID))
		}
	}

	return nil
}

func (m *Manifest) setStatus(r *Resource, s Status, lock *sync.RWMutex) {
	lock.Lock()
	if re, ok := m.resources[r.ResourceID]; ok {
		re.Status = s
		m.resources[r.ResourceID] = re
	}
	lock.Unlock()
}
