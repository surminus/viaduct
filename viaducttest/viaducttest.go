// Package viaducttest helps test resources without changing the host: a
// Runner that fakes commands, a Recorder that keeps events, and an Env rooted
// in a temporary directory.
package viaducttest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/surminus/viaduct"
)

// Runner is a fake viaduct.Runner. Commands are matched against the
// responses set with On and OnAny, most recent first. A command with no
// response fails the test.
type Runner struct {
	t testing.TB

	mu        sync.Mutex
	responses []*Response
	calls     []viaduct.Command
}

// NewRunner returns a fake runner that reports unexpected commands to t.
func NewRunner(t testing.TB) *Runner {
	return &Runner{t: t}
}

// Response is what the fake runner does for a matching command.
type Response struct {
	name string
	args []string
	any  bool

	stdout, stderr string
	code           int
	err            error
	do             func(c viaduct.Command) error
}

// On responds to a command with exactly these arguments. By default it
// succeeds with no output.
func (r *Runner) On(name string, args ...string) *Response {
	return r.add(&Response{name: name, args: args})
}

// OnAny responds to a command with any arguments.
func (r *Runner) OnAny(name string) *Response {
	return r.add(&Response{name: name, any: true})
}

func (r *Runner) add(res *Response) *Response {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.responses = append(r.responses, res)

	return res
}

// Stdout sets what the command writes to stdout.
func (res *Response) Stdout(s string) *Response {
	res.stdout = s
	return res
}

// Stderr sets what the command writes to stderr.
func (res *Response) Stderr(s string) *Response {
	res.stderr = s
	return res
}

// Exit sets the exit status. A non-zero status returns a *viaduct.ExitError.
func (res *Response) Exit(code int) *Response {
	res.code = code
	return res
}

// Fail makes the command fail to start with err.
func (res *Response) Fail(err error) *Response {
	res.err = err
	return res
}

// Do runs fn when the command runs, such as to create the file a real
// command would. An error from fn is returned as the command's error.
func (res *Response) Do(fn func(c viaduct.Command) error) *Response {
	res.do = fn
	return res
}

func (res *Response) matches(c viaduct.Command) bool {
	if res.name != c.Name {
		return false
	}

	return res.any || slices.Equal(res.args, c.Args)
}

// Run implements viaduct.Runner.
func (r *Runner) Run(ctx context.Context, c viaduct.Command) error {
	r.mu.Lock()
	r.calls = append(r.calls, c)

	var res *Response
	for i := len(r.responses) - 1; i >= 0; i-- {
		if r.responses[i].matches(c) {
			res = r.responses[i]
			break
		}
	}
	r.mu.Unlock()

	if res == nil {
		r.t.Errorf("viaducttest: unexpected command: %s", c)
		return fmt.Errorf("viaducttest: unexpected command: %s", c)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if res.err != nil {
		return res.err
	}

	if c.Stdout != nil {
		_, _ = io.WriteString(c.Stdout, res.stdout)
	}

	if c.Stderr != nil {
		_, _ = io.WriteString(c.Stderr, res.stderr)
	}

	if res.do != nil {
		if err := res.do(c); err != nil {
			return err
		}
	}

	if res.code != 0 {
		return &viaduct.ExitError{Code: res.code}
	}

	return nil
}

// Calls returns every command run so far, in order.
func (r *Runner) Calls() []viaduct.Command {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.calls)
}

// Commands returns every command run so far as a command line.
func (r *Runner) Commands() []string {
	var out []string

	for _, c := range r.Calls() {
		out = append(out, c.String())
	}

	return out
}

// Ran reports whether a command with exactly these arguments has run.
func (r *Runner) Ran(name string, args ...string) bool {
	want := viaduct.Command{Name: name, Args: args}.String()

	return slices.Contains(r.Commands(), want)
}

// Recorder is a handler that keeps every event.
type Recorder struct {
	mu     sync.Mutex
	events []viaduct.Event
}

// Handle implements viaduct.Handler.
func (r *Recorder) Handle(_ context.Context, e viaduct.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, e)
}

// Events returns every event so far.
func (r *Recorder) Events() []viaduct.Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.events)
}

// Messages returns the message of each event at the given level.
func (r *Recorder) Messages(level viaduct.Level) []string {
	var out []string

	for _, e := range r.Events() {
		if e.Level == level {
			out = append(out, e.Message)
		}
	}

	return out
}

// Env is an environment for testing a resource: a temporary root, a fake
// runner and a logger that records its events.
type Env struct {
	t testing.TB

	// Root is the temporary directory resources treat as "/".
	Root string

	// DryRun makes Context carry a dry run.
	DryRun bool

	Runner   *Runner
	Recorder *Recorder
	Log      *viaduct.Logger

	blockDevices map[string]bool
}

// NewEnv returns a test environment with an empty root.
func NewEnv(t testing.TB) *Env {
	rec := &Recorder{}

	return &Env{
		t:        t,
		Root:     t.TempDir(),
		Runner:   NewRunner(t),
		Recorder: rec,
		Log:      viaduct.NewLogger(rec, "Test", "Test"),
	}
}

// Env returns the viaduct.Env that Context carries.
func (e *Env) Env() viaduct.Env {
	devices := maps.Clone(e.blockDevices)

	return viaduct.Env{
		Root:   e.Root,
		Runner: e.Runner,
		DryRun: e.DryRun,
		BlockDevice: func(path string) bool {
			return devices[filepath.Clean(path)]
		},
	}
}

// BlockDevice creates an empty file at an absolute path under the root, and
// marks it as a block device for resources that need one, such as
// Filesystem. Contexts created before the call do not see it.
func (e *Env) BlockDevice(path string) {
	e.t.Helper()

	full := e.rooted(path)

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		e.t.Fatal(err)
	}

	if _, err := os.Stat(full); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(full, nil, 0o644); err != nil {
			e.t.Fatal(err)
		}
	}

	if e.blockDevices == nil {
		e.blockDevices = make(map[string]bool)
	}

	e.blockDevices[filepath.Clean(path)] = true
}

// Context returns a context carrying the environment.
func (e *Env) Context() context.Context {
	return viaduct.WithEnv(context.Background(), e.Env())
}

// DryRunContext returns a context carrying the environment in dry run mode,
// whatever DryRun is set to.
func (e *Env) DryRunContext() context.Context {
	env := e.Env()
	env.DryRun = true

	return viaduct.WithEnv(context.Background(), env)
}

// Path returns the location of an absolute path under the root, as
// viaduct.Env.Path does.
func (e *Env) Path(path string) string {
	return e.Env().Path(path)
}

// WriteFile writes a file at an absolute path under the root, creating its
// directory.
func (e *Env) WriteFile(path, content string) {
	e.t.Helper()

	full := e.rooted(path)

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		e.t.Fatal(err)
	}

	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// ReadFile reads a file at an absolute path under the root.
func (e *Env) ReadFile(path string) string {
	e.t.Helper()

	b, err := os.ReadFile(e.rooted(path))
	if err != nil {
		e.t.Fatal(err)
	}

	return string(b)
}

// rooted returns the location of a path under the root, failing the test for
// a relative path, which would otherwise be read or written outside it.
func (e *Env) rooted(path string) string {
	e.t.Helper()

	if !filepath.IsAbs(path) {
		e.t.Fatalf("viaducttest: %s is not an absolute path", path)
	}

	return e.Path(path)
}

// Apply runs a resource's preflight checks and then the resource, and returns
// the logger it used. A nil ctx uses Context.
func (e *Env) Apply(ctx context.Context, a viaduct.ResourceAttributes) (*viaduct.Logger, error) {
	if ctx == nil {
		ctx = e.Context()
	}

	log := viaduct.NewLogger(e.Recorder, kindOf(a), a.OperationName())

	return log, viaduct.ApplyOne(ctx, a, log)
}

func kindOf(a viaduct.ResourceAttributes) string {
	name := fmt.Sprintf("%T", a)
	return name[strings.LastIndex(name, ".")+1:]
}
