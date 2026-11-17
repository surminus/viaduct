package viaduct

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	flag "github.com/spf13/pflag"
)

// Run is the entry point for a program whose only job is to apply the
// manifest. It reads the command-line flags, applies the manifest, prints
// the outcome and exits, with a non-zero status if anything failed.
//
// A program that defines its own flags, or wants to handle the outcome
// itself, uses Apply instead.
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

	// Shared by Apply and the run's own logger, so they never write at once
	handler = serialise(handler)

	report, err := m.Apply(context.Background(), Options{
		Env:             Env{DryRun: flags.DryRun},
		Handler:         handler,
		ResourceTimeout: flags.ResourceTimeout,
	})

	// Apply has already logged anything that stopped the run before it
	// started.
	if err != nil && !errors.Is(err, ErrRunFailed) {
		return 1
	}

	l := NewLogger(handler, "Viaduct", "Run")

	duration := report.Duration.Round(time.Second).String()
	failures := collectFailures(report.Final())

	var manifestPath string

	if flags.DumpManifest {
		// A failed dump does not fail the run. A resource with a field JSON
		// cannot represent, such as a func, cannot be dumped.
		path, dumpErr := dumpManifest(m, report)

		switch {
		case dumpErr != nil && flags.JSON:
			fmt.Fprintln(os.Stderr, "manifest not written:", dumpErr)
		case dumpErr != nil:
			l.Warn("manifest-not-written", "error", dumpErr.Error())
		case !flags.JSON:
			l.progress("manifest-written", "path", path)
		}

		manifestPath = path
	}

	if flags.JSON {
		output := RunOutput{
			Status:    "success",
			Duration:  duration,
			Resources: collector.resources(report),
			Failures:  failures,
			Manifest:  manifestPath,
		}

		if err != nil {
			output.Status = "failed"
		}

		out, jsonErr := json.MarshalIndent(output, "", "  ")
		if jsonErr != nil {
			fmt.Fprintln(os.Stderr, jsonErr)
			return 1
		}

		fmt.Println(string(out))
	} else if err != nil {
		l.Warn("completed-with-errors", "duration", duration)
		printFailuresTree(failures, l)

		if !flags.DumpManifest {
			l.progress("run with --dump-manifest to see every resource, or --json for the full report")
		}
	} else {
		l.progress("completed", "duration", duration)
	}

	if err != nil {
		return 1
	}

	return 0
}

// dumpedResource is a resource as --dump-manifest writes it, with the
// outcome of the run.
type dumpedResource struct {
	*Resource
	Status Status `json:"Status,omitempty"`
	Error  string `json:"Error,omitempty"`
}

// dumpManifest writes every resource, and its outcome in the report, as JSON
// to a new file readable only by the user, returning its path.
func dumpManifest(m *Manifest, report Report) (string, error) {
	resources := make(map[ResourceID]dumpedResource, len(m.resources))
	for id, r := range m.resources {
		resources[id] = dumpedResource{Resource: r}
	}

	for _, res := range report.Final() {
		d, ok := resources[res.ID]
		if !ok {
			continue
		}

		d.Status = res.Status
		if res.Err != nil {
			d.Error = res.Err.Error()
		}

		resources[res.ID] = d
	}

	out, err := json.MarshalIndent(resources, "", "    ")
	if err != nil {
		return "", err
	}

	f, err := os.CreateTemp("", "viaduct-manifest-*.json")
	if err != nil {
		return "", err
	}

	if _, err := f.Write(out); err != nil {
		_ = f.Close()
		return "", err
	}

	return f.Name(), f.Close()
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

// resources returns a result for each resource in the report, sorted by ID.
func (c *jsonCollector) resources(report Report) []ResourceResult {
	out := make([]ResourceResult, 0, len(report.Results))

	for _, r := range report.Final() {
		res := ResourceResult{
			ResourceID:   string(r.ID),
			ResourceKind: string(r.Kind),
			Description:  r.Description,
			Operation:    r.Operation,
			Status:       string(r.Status),
			Changed:      r.Changed,
			Cause:        string(r.Cause),
			Duration:     r.Duration.String(),
			Log:          c.entries[r.ID],
		}

		if r.Err != nil {
			res.Error = r.Err.Error()
		}

		out = append(out, res)
	}

	sort.SliceStable(out, func(i, j int) bool {
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
	Changed      bool       `json:"changed"`
	Cause        string     `json:"cause,omitempty"`
	Duration     string     `json:"duration"`
	Error        string     `json:"error,omitempty"`
	Log          []LogEntry `json:"log,omitempty"`
}

// RunOutput is the top-level JSON output for a run.
type RunOutput struct {
	// Status is "success" or "failed".
	Status string `json:"status"`

	Duration  string           `json:"duration"`
	Resources []ResourceResult `json:"resources"`
	Failures  []failureSummary `json:"failures,omitempty"`

	// Manifest is where --dump-manifest wrote the manifest.
	Manifest string `json:"manifest,omitempty"`
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

func toDependent(r Result) failureDependent {
	d := failureDependent{
		ResourceID:   string(r.ID),
		ResourceKind: string(r.Kind),
		Description:  r.Description,
		Operation:    r.Operation,
		Status:       string(r.Status),
	}

	if r.Err != nil {
		d.Error = r.Err.Error()
	}

	return d
}

func toSummary(r Result) failureSummary {
	d := toDependent(r)

	return failureSummary{
		ResourceID:   d.ResourceID,
		ResourceKind: d.ResourceKind,
		Description:  d.Description,
		Operation:    d.Operation,
		Status:       d.Status,
		Error:        d.Error,
	}
}

// collectFailures groups the failures that failed the run under the resource
// that failed in its own right, following each DependencyFailed result's
// Cause. A failure with no cause to follow, such as a resource that never
// started because the run was cancelled, is listed on its own after the
// rest.
func collectFailures(final []Result) []failureSummary {
	byID := make(map[ResourceID]Result, len(final))
	for _, r := range final {
		byID[r.ID] = r
	}

	// rootOf follows the causes back to a resource that failed in its own
	// right, if there is one.
	rootOf := func(r Result) (Result, bool) {
		seen := map[ResourceID]bool{}

		for r.Status == StatusDependencyFailed && r.Cause != "" && !seen[r.ID] {
			seen[r.ID] = true

			next, ok := byID[r.Cause]
			if !ok {
				return Result{}, false
			}

			r = next
		}

		return r, r.Status == StatusFailed
	}

	roots := map[ResourceID]*failureSummary{}

	rootFor := func(r Result) *failureSummary {
		s, ok := roots[r.ID]
		if !ok {
			summary := toSummary(r)
			s = &summary
			roots[r.ID] = s
		}

		return s
	}

	var orphans []failureSummary

	for _, r := range final {
		switch r.Status {
		case StatusFailed:
			rootFor(r)
		case StatusDependencyFailed:
			root, ok := rootOf(r)
			if !ok {
				orphans = append(orphans, toSummary(r))
				continue
			}

			s := rootFor(root)
			s.Dependents = append(s.Dependents, toDependent(r))
		}
	}

	out := make([]failureSummary, 0, len(roots)+len(orphans))

	for _, s := range roots {
		slices.SortFunc(s.Dependents, func(a, b failureDependent) int {
			return strings.Compare(a.ResourceID, b.ResourceID)
		})

		out = append(out, *s)
	}

	byResourceID := func(a, b failureSummary) int {
		return strings.Compare(a.ResourceID, b.ResourceID)
	}

	slices.SortFunc(out, byResourceID)
	slices.SortFunc(orphans, byResourceID)

	return append(out, orphans...)
}

func printFailuresTree(failures []failureSummary, l *Logger) {
	var b strings.Builder

	b.WriteString("Failed resources:\n")

	for _, f := range failures {
		fmt.Fprintf(&b, "\n  %s [%s] %s (%s)\n", f.ResourceKind, f.Operation, f.Description, f.ResourceID)
		fmt.Fprintf(&b, "  Error: %s\n", f.Error)

		for i, d := range f.Dependents {
			isLast := i == len(f.Dependents)-1
			prefix := "├──"
			if isLast {
				prefix = "└──"
			}
			fmt.Fprintf(&b, "    %s %s [%s] %s (%s): %s\n", prefix, d.ResourceKind, d.Operation, d.Description, d.ResourceID, d.Error)
		}
	}

	l.Error(b.String())
}
