package viaduct

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Status string

const (
	// Statuses
	DependencyFailed Status = "DependencyFailed"
	Failed           Status = "Failed"
	Pending          Status = "Pending"
	Success          Status = "Success"
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

func (m *Manifest) addResource(r *Resource, a ResourceAttributes) (err error) {
	// Set attributes
	r.Attributes = a

	if a.Params().GlobalLock {
		r.GlobalLock = true
	}

	// Create a string representation of our resource
	if err := r.setID(); err != nil {
		return err
	}

	if _, ok := m.resources[r.ResourceID]; ok {
		return fmt.Errorf("resource already exists:\n%s", attrJSON(r))
	}

	m.resources[r.ResourceID] = *r

	return err
}

func (m *Manifest) Add(attributes ResourceAttributes, deps ...*Resource) *Resource {
	log := NewLogger("Viaduct", "Compile")

	r, err := newResource(deps)
	if err != nil {
		log.Fatal(err)
	}

	if err := r.init(attributes); err != nil {
		log.Fatal(err)
	}

	if err := m.addResource(r, attributes); err != nil {
		log.Fatal(err)
	}

	return r
}

func attrJSON(a any) string {
	str, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	return string(str)
}

// Run will run the specified resources concurrently, taking into account
// any dependencies that have been declared
func (m *Manifest) Run() {
	log := NewLogger("Viaduct", "Run")
	start := time.Now()
	log.Info("Started")

	log.Info("Performing preflight checks...")

	var preflightFailed bool
	for id, resource := range m.resources {
		if err := resource.preflight(); err != nil {
			if r, ok := m.resources[id]; ok {
				r.Error.Err = err
				r.Error.Message = err.Error()
				m.resources[id] = r
			}

			preflightFailed = true
		}
	}

	if preflightFailed {
		for _, resource := range m.resources {
			if resource.Error.Err != nil {
				log.Critical(
					fmt.Sprintf(
						"The following resource failed preflight checks:\n%s\n",
						attrJSON(resource),
					),
				)
			}
		}

		os.Exit(1)
	}

	var lock, globalLock sync.RWMutex
	var wg sync.WaitGroup

	wg.Add(len(m.resources))

	for id, resource := range m.resources {
		go m.apply(id, resource, &wg, &lock, &globalLock)
	}

	wg.Wait()

	timeTaken := time.Since(start).Round(time.Second).String()

	var withErrors bool
	for _, resource := range m.resources {
		if resource.Error.Err != nil {
			withErrors = true
			break
		}
	}

	if withErrors {
		log.Warn(fmt.Sprintf("Completed with errors in %s:", timeTaken))
	} else {
		log.Info(fmt.Sprintf("Completed without errors in %s:", timeTaken))
	}

	if withErrors {
		for _, resource := range m.resources {
			if resource.Error.Err != nil {
				log.Critical(
					fmt.Sprintf(
						"The following resource returned an error:\n%s\n",
						attrJSON(resource),
					),
				)
			}
		}
	}

	if Cli.DumpManifest {
		tmpName := fmt.Sprintf("/tmp/viaduct-%d.json", time.Now().Unix())

		out, err := json.MarshalIndent(m.resources, "", "    ")
		if err != nil {
			log.Fatal(err)
		}

		err = os.WriteFile(tmpName, out, 0o644)
		if err != nil {
			log.Fatal(err)
		}

		log.Info(fmt.Sprintf("Manifest written to: %s", tmpName))
	}

	if withErrors {
		if !Cli.DumpManifest {
			log.Info("To see all resources, run with --dump-manifest")
		}
		os.Exit(1)
	}

	// Tidy up temporary directory if there were no errors
	err := os.RemoveAll(filepath.Join(Attribute.TmpDir))
	if err != nil {
		log.Fatal(err)
	}
}

func (m *Manifest) apply(id ResourceID, r Resource, wg *sync.WaitGroup, lock *sync.RWMutex, globalLock *sync.RWMutex) {
	defer wg.Done()

	err := m.dependencyCheck(&r, lock)
	if err != nil {
		m.setError(&r, lock, err)
		return
	}

	if r.GlobalLock {
		globalLock.Lock()
		defer globalLock.Unlock()
	}

	// Run the resource operation
	err = r.run()
	if err != nil {
		m.setStatus(&r, lock, Failed)
		m.setError(&r, lock, err)
		return
	}

	m.setStatus(&r, lock, Success)
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
					if d.Failed() {
						lock.RUnlock()
						m.setStatus(r, lock, DependencyFailed)

						// Unlock and return error
						return fmt.Errorf("upstream dependency %s returned an error", d.ResourceID)
					}

					if d.Status != Success {
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

func (m *Manifest) setStatus(r *Resource, lock *sync.RWMutex, s Status) {
	lock.Lock()
	if re, ok := m.resources[r.ResourceID]; ok {
		re.Status = s
		m.resources[r.ResourceID] = re
	}
	lock.Unlock()
}

func (m *Manifest) setError(r *Resource, lock *sync.RWMutex, err error) {
	lock.Lock()
	if re, ok := m.resources[r.ResourceID]; ok {
		re.Error = Error{Err: err, Message: err.Error()}
		m.resources[r.ResourceID] = re
	}
	lock.Unlock()
}
