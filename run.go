package viaduct

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	flag "github.com/spf13/pflag"
)

// Run is the entry point for a program whose only job is to apply the
// manifest. It reads the command-line flags, runs the resources concurrently
// once their dependencies have succeeded, prints the outcome and exits, with
// a non-zero status if anything failed.
func (m *Manifest) Run() {
	os.Exit(m.run())
}

// run is Run without the exit, returning the exit code.
func (m *Manifest) run() int {
	flags, err := ParseFlags()
	if code, exit := flagsExit(err, os.Stderr); exit {
		return code
	}

	return m.runWith(flags)
}

// runWith applies the manifest as Run does, with the flags already read. It
// returns the exit code.
func (m *Manifest) runWith(flags CliFlags) int {
	if flags.Attributes {
		out, err := Attributes().JSON()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		fmt.Println(out)

		return 0
	}

	defer func() {
		_ = removeTmpDir()
	}()

	l := NewLogger("Viaduct", "Run")

	if _, err := LoadAttributes(); err != nil {
		l.Error("attributes", "error", err.Error())
		return 1
	}

	if flags.DryRun {
		log.Println("WARNING: dry run mode enabled")
	}

	start := time.Now()
	l.Info("started")
	l.Info("preflight-checks")

	if flags.JSON {
		m.collector = newResultCollector()
	}

	// A cycle can never make progress, so fail before any resource does work
	// rather than waiting for the dependency timeout to notice.
	if err := m.dependencyCycle(); err != nil {
		l.Error("dependency-cycle", "error", err.Error())
		return 1
	}

	var preflightFailed bool
	for id, resource := range m.resources {
		if err := resource.preflight(); err != nil {
			if r, ok := m.resources[id]; ok {
				r.Err = err
				r.Message = err.Error()
				m.resources[id] = r
			}

			preflightFailed = true
		}
	}

	if preflightFailed {
		for _, resource := range m.resources {
			if resource.Err != nil {
				l.Error("preflight-failed",
					"resource_id", string(resource.ResourceID),
					"resource_kind", string(resource.ResourceKind),
					"error", resource.Message,
				)
			}
		}

		return 1
	}

	var lock sync.RWMutex
	var wg sync.WaitGroup

	locks := newLockSet()

	// Take a copy before starting anything, because each resource writes its
	// status back into m.resources as it runs. Ranging over the map while
	// those writes happen is a fatal concurrent map iteration and write.
	resources := make([]Resource, 0, len(m.resources))
	for _, resource := range m.resources {
		resources = append(resources, resource)
	}

	wg.Add(len(resources))

	for _, resource := range resources {
		go m.apply(resource, &wg, &lock, locks)
	}

	wg.Wait()

	timeTaken := time.Since(start).Round(time.Second).String()

	// Whether the run failed and what gets reported come from the same place,
	// so a resource can never fail the run without being named.
	failures := collectFailures(m.resources)
	withErrors := len(failures) > 0

	if flags.JSON {
		status := "success"
		if withErrors {
			status = "failed"
		}

		output := RunOutput{
			Status:    status,
			Duration:  timeTaken,
			Resources: m.collector.Results(),
			Failures:  failures,
		}

		out, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(string(out))

		if withErrors {
			return 1
		}
	} else {
		if withErrors {
			l.Warn("completed-with-errors", "duration", timeTaken)
		} else {
			l.Info("completed", "duration", timeTaken)
		}

		if withErrors {
			printFailuresTree(failures, l)
		}
	}

	if flags.DumpManifest {
		tmpName := fmt.Sprintf("/tmp/viaduct-%d.json", time.Now().Unix())

		out, err := json.MarshalIndent(m.resources, "", "    ")
		if err != nil {
			l.Error(err.Error())
			return 1
		}

		err = os.WriteFile(tmpName, out, 0o644)
		if err != nil {
			l.Error(err.Error())
			return 1
		}

		l.Info("manifest-written", "path", tmpName)
	}

	if withErrors {
		if !flags.DumpManifest && !flags.JSON {
			l.Info("hint", "msg", "to see all resources, run with --dump-manifest")
		}
		return 1
	}

	return 0
}

// flagsExit returns the exit code for an error from reading the flags, and
// whether to exit with it.
func flagsExit(err error, stderr io.Writer) (int, bool) {
	switch {
	case err == nil:
		return 0, false
	case errors.Is(err, flag.ErrHelp):
		return 0, true
	default:
		fmt.Fprintln(stderr, err)
		return 2, true
	}
}

// ResultCollector gathers per-resource results from concurrent goroutines.
type ResultCollector struct {
	mu      sync.Mutex
	results []ResourceResult
}

func newResultCollector() *ResultCollector {
	return &ResultCollector{}
}

func (c *ResultCollector) Add(r ResourceResult) {
	c.mu.Lock()
	c.results = append(c.results, r)
	c.mu.Unlock()
}

func (c *ResultCollector) Results() []ResourceResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]ResourceResult, len(c.results))
	copy(out, c.results)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ResourceID < out[j].ResourceID
	})
	return out
}

// ResourceResult is a single resource's outcome in a run.
type ResourceResult struct {
	ResourceID   string     `json:"resource_id"`
	ResourceKind string     `json:"resource_kind"`
	Description  string     `json:"description"`
	Operation    string     `json:"operation"`
	Status       string     `json:"status"`
	Error        string     `json:"error,omitempty"`
	Log          []LogEntry `json:"log,omitempty"`
}

// RunOutput is the top-level JSON output for a run.
type RunOutput struct {
	Status    string           `json:"status"`
	Duration  string           `json:"duration"`
	Resources []ResourceResult `json:"resources"`
	Failures  []failureSummary `json:"failures,omitempty"`
}

type failureDependent struct {
	ResourceID   string `json:"resource_id"`
	ResourceKind string `json:"resource_kind"`
	Description  string `json:"description"`
	Operation    string `json:"operation"`
	Status       string `json:"status"`
	Error        string `json:"error"`
}

type failureSummary struct {
	ResourceID   string             `json:"resource_id"`
	ResourceKind string             `json:"resource_kind"`
	Description  string             `json:"description"`
	Operation    string             `json:"operation"`
	Status       string             `json:"status"`
	Error        string             `json:"error"`
	Dependents   []failureDependent `json:"dependents,omitempty"`
}

func resourceToDependent(r Resource) failureDependent {
	return failureDependent{
		ResourceID:   string(r.ResourceID),
		ResourceKind: string(r.ResourceKind),
		Description:  r.Attributes.Description(),
		Operation:    r.Attributes.OperationName(),
		Status:       string(r.Status),
		Error:        r.Message,
	}
}

// collectFailures groups failed resources into root failures and their
// cascading dependency failures.
func collectFailures(resources map[ResourceID]Resource) []failureSummary {
	// Split into root failures and dependency failures.
	var roots []Resource
	depFailed := make(map[ResourceID]Resource)

	for _, r := range resources {
		switch {
		case r.Status == StatusDependencyFailed:
			depFailed[r.ResourceID] = r
		case r.Status == StatusFailed, r.Err != nil:
			// A resource that recorded an error without a status is still a
			// failure, and reporting it as a root is better than dropping it
			// and leaving the run to fail with nothing to look at.
			roots = append(roots, r)
		}
	}

	// Sort roots by ID for stable output.
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].ResourceID < roots[j].ResourceID
	})

	// Build a set of all root IDs for quick lookup.
	rootIDs := make(map[ResourceID]bool)
	for _, r := range roots {
		rootIDs[r.ResourceID] = true
	}

	// For each dependency-failed resource, trace back to find which root
	// failure it depends on (directly or transitively).
	claimed := make(map[ResourceID]ResourceID) // dep -> root
	for id, r := range depFailed {
		if root := traceToRoot(r, resources, rootIDs); root != "" {
			claimed[id] = root
		}
	}

	// Build summaries.
	var summaries []failureSummary
	for _, r := range roots {
		s := failureSummary{
			ResourceID:   string(r.ResourceID),
			ResourceKind: string(r.ResourceKind),
			Description:  r.Attributes.Description(),
			Operation:    r.Attributes.OperationName(),
			Status:       string(r.Status),
			Error:        r.Message,
		}

		// Collect dependents claimed by this root.
		var deps []failureDependent
		for depID, rootID := range claimed {
			if rootID == r.ResourceID {
				deps = append(deps, resourceToDependent(depFailed[depID]))
			}
		}
		sort.Slice(deps, func(i, j int) bool {
			return deps[i].ResourceID < deps[j].ResourceID
		})
		s.Dependents = deps

		summaries = append(summaries, s)
	}

	// Any unclaimed dependency failures get listed as their own entries.
	var orphans []Resource
	for id, r := range depFailed {
		if _, ok := claimed[id]; !ok {
			orphans = append(orphans, r)
		}
	}
	sort.Slice(orphans, func(i, j int) bool {
		return orphans[i].ResourceID < orphans[j].ResourceID
	})
	for _, r := range orphans {
		summaries = append(summaries, failureSummary{
			ResourceID:   string(r.ResourceID),
			ResourceKind: string(r.ResourceKind),
			Description:  r.Attributes.Description(),
			Operation:    r.Attributes.OperationName(),
			Status:       string(r.Status),
			Error:        r.Message,
		})
	}

	return summaries
}

// traceToRoot follows a dependency-failed resource's DependsOn chain to find
// a root failure. Returns the root's ResourceID, or empty if none found.
func traceToRoot(r Resource, resources map[ResourceID]Resource, rootIDs map[ResourceID]bool) ResourceID {
	seen := make(map[ResourceID]bool)
	queue := []ResourceID{r.ResourceID}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if seen[current] {
			continue
		}
		seen[current] = true

		res, ok := resources[current]
		if !ok {
			continue
		}

		for _, dep := range res.DependsOn {
			if rootIDs[dep] {
				return dep
			}
			queue = append(queue, dep)
		}
	}

	return ""
}

func printFailuresTree(failures []failureSummary, l *Logger) {
	var b strings.Builder

	b.WriteString("Failed resources:\n")

	for _, f := range failures {
		fmt.Fprintf(&b, "\n  %s [%s] %s\n", f.ResourceKind, f.Operation, f.Description)
		fmt.Fprintf(&b, "  Error: %s\n", f.Error)

		for i, d := range f.Dependents {
			isLast := i == len(f.Dependents)-1
			prefix := "├──"
			if isLast {
				prefix = "└──"
			}
			fmt.Fprintf(&b, "    %s %s [%s] %s: %s\n", prefix, d.ResourceKind, d.Operation, d.Description, d.Error)
		}
	}

	l.Error(b.String())
}
