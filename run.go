package viaduct

import (
	"context"
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

	"github.com/fatih/color"
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

	var (
		handler   Handler
		collector *jsonCollector
	)

	switch {
	case flags.JSON:
		collector = newJSONCollector()
		handler = collector
	case flags.Silent:
		handler = DiscardHandler
	default:
		handler = NewTextHandler(TextOptions{Stdout: flags.Stdout, Quiet: flags.Quiet})
	}

	// Shared by the resources and the run's own logger, so they never write
	// at once
	handler = serialise(handler)
	m.handler = handler

	if len(m.errs) > 0 {
		l := NewLogger(handler, "Viaduct", "Compile")
		for _, err := range m.errs {
			l.Error(err.Error())
		}

		return 1
	}

	l := NewLogger(handler, "Viaduct", "Run")

	if _, err := LoadAttributes(); err != nil {
		l.Error("attributes", "error", err.Error())
		return 1
	}

	if flags.DryRun {
		log.Println("WARNING: dry run mode enabled")
	}

	start := time.Now()
	l.progress("started")
	l.progress("preflight-checks")

	// A cycle can never progress, so fail before any resource does work.
	if err := m.compile(); err != nil {
		l.Error("invalid-manifest", "error", err.Error())
		return 1
	}

	var preflightFailed bool
	for _, resource := range m.order {
		if err := resource.preflight(handler); err != nil {
			resource.Err = err
			resource.Message = err.Error()

			preflightFailed = true
		}
	}

	if preflightFailed {
		for _, resource := range m.order {
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

	wg.Add(len(m.order))

	for _, resource := range m.order {
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
			Resources: collector.resources(m.resources),
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
			l.progress("completed", "duration", timeTaken)
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

		l.progress("manifest-written", "path", tmpName)
	}

	if withErrors {
		if !flags.DumpManifest && !flags.JSON {
			l.progress("hint", "msg", "to see all resources, run with --dump-manifest")
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

// TextOptions configures a TextHandler.
type TextOptions struct {
	// Stdout writes everything except warnings and errors to Out rather
	// than Err. Command output keeps its own stream either way.
	Stdout bool

	// Quiet drops everything except warnings, errors and command output on
	// stderr.
	Quiet bool

	// Out and Err are where output goes. The defaults are os.Stdout and
	// os.Stderr.
	Out, Err io.Writer
}

// NewTextHandler returns the handler Run uses, which prints a line per event:
//
//	  OK  File [Create]          created path=/home/user/.bashrc
//	  --  File [Create]          up-to-date path=/home/user/.bashrc
//	FAIL  Execute [Run]          command failed command="echo hello"
//
// Command output is printed as it is, to the stream it came from.
func NewTextHandler(o TextOptions) Handler {
	if o.Out == nil {
		o.Out = os.Stdout
	}

	if o.Err == nil {
		o.Err = os.Stderr
	}

	return &textHandler{o: o}
}

type textHandler struct {
	o TextOptions
}

func (h *textHandler) Handle(_ context.Context, e Event) {
	info := h.o.Err
	if h.o.Stdout {
		info = h.o.Out
	}

	switch e.Level {
	case LevelOutput:
		stream, _ := e.Field("stream")
		if stream == Stdout {
			if !h.o.Quiet {
				fmt.Fprintln(h.o.Out, e.Message)
			}
		} else {
			fmt.Fprintln(h.o.Err, e.Message)
		}
	case LevelInfo:
		if !h.o.Quiet {
			fmt.Fprintln(info, formatLine(okTag, e))
		}
	case LevelChanged:
		if !h.o.Quiet {
			fmt.Fprintln(info, formatLine(okTag, e))
		}
	case LevelNoop:
		if !h.o.Quiet {
			fmt.Fprintln(info, formatLine(noopTag, e))
		}
	case LevelWarn:
		fmt.Fprintln(h.o.Err, formatLine(warnTag, e))
	case LevelError:
		fmt.Fprintln(h.o.Err, formatLine(failTag, e))
	}
}

// resourceActionWidth is the width of the "Kind [Operation]" column.
const resourceActionWidth = 22

// formatLine builds a human-readable output line:
//
//	  OK  File [Create] created path=/home/user/.bashrc
//	  --  File [Create] up-to-date path=/home/user/.bashrc
//	FAIL  Execute [Run] command failed command="echo hello"
func formatLine(tag string, e Event) string {
	var b strings.Builder

	ra := fmt.Sprintf("%s [%s]", e.Kind, e.Operation)
	fmt.Fprintf(&b, "%s  %-*s %s", tag, resourceActionWidth, ra, e.Message)

	for _, f := range e.Fields {
		fmt.Fprintf(&b, " %s=%s", f.Key, quoteIfNeeded(fmt.Sprint(f.Value)))
	}

	return b.String()
}

// quoteIfNeeded wraps a value in double quotes if it contains spaces or
// special characters.
func quoteIfNeeded(v string) string {
	if strings.ContainsAny(v, " \t\n\"=") {
		return fmt.Sprintf("%q", v)
	}
	return v
}

var (
	okTag   = color.New(color.FgGreen).Sprintf("%4s", "OK")
	noopTag = color.New(color.FgBlue, color.Faint).Sprintf("%4s", "--")
	warnTag = color.New(color.FgYellow).Sprintf("%4s", "WARN")
	failTag = color.New(color.FgRed).Sprintf("%4s", "FAIL")
)

// LogEntry is a single event in the JSON output of Run.
type LogEntry struct {
	Level   string            `json:"level"`
	Message string            `json:"msg"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// jsonCollector keeps each resource's events for the JSON output of Run.
// Command output and events about the run itself are left out.
type jsonCollector struct {
	entries map[ResourceID][]LogEntry
}

func newJSONCollector() *jsonCollector {
	return &jsonCollector{entries: make(map[ResourceID][]LogEntry)}
}

var jsonLevels = map[Level]string{
	LevelChanged: "OK",
	LevelNoop:    "NOOP",
	LevelWarn:    "WARN",
	LevelError:   "ERR",
}

func (c *jsonCollector) Handle(_ context.Context, e Event) {
	level, ok := jsonLevels[e.Level]
	if !ok || e.ResourceID == "" {
		return
	}

	entry := LogEntry{Level: level, Message: e.Message}

	if len(e.Fields) > 0 {
		entry.Fields = make(map[string]string, len(e.Fields))
		for _, f := range e.Fields {
			entry.Fields[f.Key] = fmt.Sprint(f.Value)
		}
	}

	c.entries[e.ResourceID] = append(c.entries[e.ResourceID], entry)
}

// resources returns a result for each resource, sorted by ID.
func (c *jsonCollector) resources(rs map[ResourceID]*Resource) []ResourceResult {
	out := make([]ResourceResult, 0, len(rs))

	for _, r := range rs {
		out = append(out, ResourceResult{
			ResourceID:   string(r.ResourceID),
			ResourceKind: string(r.ResourceKind),
			Description:  r.Attributes.Description(),
			Operation:    r.Attributes.OperationName(),
			Status:       string(r.Status),
			Error:        r.Message,
			Log:          c.entries[r.ResourceID],
		})
	}

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

func resourceToDependent(r *Resource) failureDependent {
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
func collectFailures(resources map[ResourceID]*Resource) []failureSummary {
	// Split into root failures and dependency failures.
	var roots []*Resource
	depFailed := make(map[ResourceID]*Resource)

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
	var orphans []*Resource
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
func traceToRoot(r *Resource, resources map[ResourceID]*Resource, rootIDs map[ResourceID]bool) ResourceID {
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
