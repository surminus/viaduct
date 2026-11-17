package viaduct

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecRunner(t *testing.T) {
	ctx := context.Background()

	t.Run("output and exit status", func(t *testing.T) {
		rec := &recorder{}
		log := NewLogger(rec, "Test", "Test")

		out, err := Command{Name: "sh", Args: []string{"-c", "echo out; echo err >&2; exit 3"}}.Output(ctx, log)

		assert.Equal(t, "out\n", string(out))
		code, ok := ExitCode(err)
		assert.True(t, ok)
		assert.Equal(t, 3, code)
		assert.Equal(t, []string{"err"}, rec.messages(LevelOutput))
	})

	t.Run("working directory, environment and stdin", func(t *testing.T) {
		dir := t.TempDir()

		out, err := Command{
			Name:  "sh",
			Args:  []string{"-c", `pwd; echo "$VIADUCT_TEST"; cat`},
			Dir:   dir,
			Env:   []string{"VIADUCT_TEST=set"},
			Stdin: strings.NewReader("input"),
		}.Output(ctx, NewLogger(nil, "Test", "Test"))
		require.NoError(t, err)

		resolved, err := filepath.EvalSymlinks(dir)
		require.NoError(t, err)
		assert.Equal(t, resolved+"\nset\ninput", string(out))
	})

	t.Run("a missing program is not an exit status", func(t *testing.T) {
		err := Command{Name: "viaduct-does-not-exist"}.Run(ctx, NewLogger(nil, "Test", "Test"))
		require.Error(t, err)

		_, ok := ExitCode(err)
		assert.False(t, ok)
	})

	t.Run("cancelling stops the command and its children", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "marker")

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := Command{Name: "sh", Args: []string{"-c", "(sleep 1; touch " + marker + ") & sleep 30"}}.Run(ctx, NewLogger(nil, "Test", "Test"))

		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(start), 10*time.Second)

		time.Sleep(1500 * time.Millisecond)
		assert.NoFileExists(t, marker, "the child of the shell was stopped too")
	})

	t.Run("children that ignore SIGTERM are killed", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "marker")

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		// The child ignores SIGTERM, and redirects its output so nothing
		// holds the pipe open
		script := "(trap '' TERM; sleep 4; touch " + marker + ") >/dev/null 2>&1 & sleep 30"
		err := Command{Name: "sh", Args: []string{"-c", script}}.Run(ctx, NewLogger(nil, "Test", "Test"))
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		time.Sleep(5 * time.Second)
		assert.NoFileExists(t, marker, "the child that ignored SIGTERM was killed")
	})

	t.Run("a background process holding the output open does not hang", func(t *testing.T) {
		start := time.Now()

		err := Command{Name: "sh", Args: []string{"-c", "sleep 30 &"}}.Run(ctx, NewLogger(nil, "Test", "Test"))
		require.NoError(t, err)

		assert.Less(t, time.Since(start), 10*time.Second)
	})

	t.Run("output stops being collected after the wait delay", func(t *testing.T) {
		start := time.Now()

		// Output writes to a bytes.Buffer, which has no lock, so the race
		// detector catches a write after Output has returned
		out, err := Command{Name: "sh", Args: []string{"-c", "echo first; (sleep 3; echo late) &"}}.Output(ctx, nil)
		require.NoError(t, err)

		elapsed := time.Since(start)
		assert.GreaterOrEqual(t, elapsed, commandWaitDelay)
		assert.Less(t, elapsed, commandWaitDelay+time.Second)

		time.Sleep(2 * time.Second)
		assert.Equal(t, "first\n", string(out))
	})

	t.Run("a command is given its grace to stop after SIGTERM", func(t *testing.T) {
		withCancelGrace(t, 5*time.Second)

		marker := filepath.Join(t.TempDir(), "marker")

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		// Takes longer to clean up than the delay for output after exiting
		script := "trap 'sleep 2.5; touch " + marker + "; exit 1' TERM; while :; do sleep 0.05; done"
		err := Command{Name: "sh", Args: []string{"-c", script}}.Run(ctx, nil)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.FileExists(t, marker, "the command finished cleaning up")
	})

	t.Run("a command that exits 0 on SIGTERM has still failed", func(t *testing.T) {
		withCancelGrace(t, 5*time.Second)

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		err := Command{Name: "sh", Args: []string{"-c", "trap 'exit 0' TERM; while :; do sleep 0.05; done"}}.Run(ctx, nil)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("a command that ignores SIGTERM is killed after its grace", func(t *testing.T) {
		withCancelGrace(t, 200*time.Millisecond)

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := Command{Name: "sh", Args: []string{"-c", "trap '' TERM; while :; do sleep 0.05; done"}}.Run(ctx, nil)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(start), 5*time.Second)
	})
}

// withCancelGrace sets how long commands have to stop after SIGTERM for the
// rest of the test
func withCancelGrace(t *testing.T, d time.Duration) {
	orig := commandCancelGrace
	commandCancelGrace = d

	t.Cleanup(func() { commandCancelGrace = orig })
}

func TestExitCode(t *testing.T) {
	code, ok := ExitCode(&ExitError{Code: 2})
	assert.True(t, ok)
	assert.Equal(t, 2, code)

	_, ok = ExitCode(errors.New("other"))
	assert.False(t, ok)

	_, ok = ExitCode(nil)
	assert.False(t, ok)

	assert.EqualError(t, &ExitError{Code: 2}, "exit status 2")
}

func TestEnv(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		env := EnvFrom(context.Background())

		assert.False(t, env.DryRun)
		assert.Empty(t, env.Root)
		assert.Equal(t, ExecRunner, env.Runner)
		assert.Equal(t, "/etc/fstab", env.Path("/etc/fstab"))
	})

	t.Run("root", func(t *testing.T) {
		env := EnvFrom(WithEnv(context.Background(), Env{Root: "/tmp/root"}))

		assert.Equal(t, "/tmp/root/etc/fstab", env.Path("/etc/fstab"))
		assert.Equal(t, "/tmp/root/proc/sys/vm/swappiness", env.Path("/proc/sys/vm/swappiness"))
		assert.Equal(t, "relative", env.Path("relative"))
		assert.True(t, env.IsRoot())
	})

	t.Run("paths cannot escape the root", func(t *testing.T) {
		env := Env{Root: "/tmp/root"}

		assert.Equal(t, "/tmp/root/etc/passwd", env.Path("/../etc/passwd"))
		assert.Equal(t, "/tmp/root/etc/passwd", env.Path("/etc/../../../etc/passwd"))
		assert.Equal(t, "/tmp/root", env.Path("/.."))
	})
}

// envResource records the environment it ran in
type envResource struct {
	testResourceType
	env Env
}

func (e *envResource) Run(ctx context.Context, log *Logger) error {
	e.env = EnvFrom(ctx)
	return nil
}

// countingRunner counts the commands it is asked to run, and runs none
type countingRunner struct {
	calls atomic.Int32
}

func (c *countingRunner) Run(context.Context, Command) error {
	c.calls.Add(1)
	return nil
}

// ctxResource waits for its context to be cancelled
type ctxResource struct {
	testResourceType
}

func (c *ctxResource) Run(ctx context.Context, log *Logger) error {
	<-ctx.Done()
	return ctx.Err()
}

// quietStopResource waits for its context to be cancelled, then returns nil
type quietStopResource struct {
	testResourceType
}

func (q *quietStopResource) Run(ctx context.Context, log *Logger) error {
	<-ctx.Done()
	return nil
}

// paramsTimeoutResource asks for its own timeout
type paramsTimeoutResource struct {
	testResourceType
	timeout time.Duration
}

func (p *paramsTimeoutResource) Params() *ResourceParams {
	return &ResourceParams{Timeout: p.timeout}
}

func TestApplyEnv(t *testing.T) {
	t.Run("options reach the resource", func(t *testing.T) {
		runner := &countingRunner{}
		r := &envResource{}

		m := New()
		m.Add(r)

		_, err := m.Apply(context.Background(), Options{Env: Env{DryRun: true, Root: "/tmp/root", Runner: runner}})
		require.NoError(t, err)

		assert.True(t, r.env.DryRun)
		assert.Equal(t, "/tmp/root", r.env.Root)
		assert.Same(t, runner, r.env.Runner)
	})

	t.Run("a root without a runner is an error", func(t *testing.T) {
		r := &envResource{}

		m := New()
		m.Add(r)

		_, err := m.Apply(context.Background(), Options{Env: Env{Root: t.TempDir()}})
		require.ErrorIs(t, err, ErrInvalidManifest)
		assert.ErrorContains(t, err, "without a Runner")
		assert.Nil(t, r.env.Runner, "nothing ran")
	})

	t.Run("a timeout cancels the resource's context", func(t *testing.T) {
		later := newTestResource("later")

		m := New()
		a := m.Add(&ctxResource{})
		m.Add(later, a)
		m.SetResourceTimeout(20 * time.Millisecond)

		report, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrRunFailed)

		byID := map[ResourceID]Result{}
		for _, r := range report.Results {
			byID[r.ID] = r
		}

		assert.ErrorIs(t, byID[a.ResourceID].Err, context.DeadlineExceeded)
		assert.ErrorContains(t, byID[a.ResourceID].Err, "timed out after 20ms")
		assert.NotErrorIs(t, byID[a.ResourceID].Err, errAbandoned, "a resource that stops is not abandoned")
	})

	t.Run("returning nil once cancelled is not success", func(t *testing.T) {
		m := New()
		a := m.Add(&quietStopResource{})
		m.SetResourceTimeout(20 * time.Millisecond)

		report, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrRunFailed)

		res := byID(report)[a.ResourceID]
		assert.Equal(t, StatusFailed, res.Status)
		assert.ErrorIs(t, res.Err, context.DeadlineExceeded)
		assert.ErrorContains(t, res.Err, "timed out after 20ms")
	})

	t.Run("the caller's deadline is not the resource's timeout", func(t *testing.T) {
		m := New()
		a := m.Add(&ctxResource{})
		m.SetResourceTimeout(-1)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		report, err := m.Apply(ctx, Options{})
		require.Error(t, err)

		res := byID(report)[a.ResourceID]
		assert.ErrorIs(t, res.Err, context.DeadlineExceeded)
		assert.NotContains(t, res.Err.Error(), "timed out")
	})

	t.Run("a resource that stops on time does not stop the rest of the run", func(t *testing.T) {
		other := newTestResource("other")

		m := New()
		m.Add(&ctxResource{})
		m.SetResourceTimeout(20 * time.Millisecond)

		// other has no dependency on the failure, and is not held up by it
		m.Add(other)

		_, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrRunFailed)
		assert.True(t, other.ran.Load())
	})

	t.Run("params timeout", func(t *testing.T) {
		m := New()
		r := m.Add(&paramsTimeoutResource{timeout: 7 * time.Minute})
		assert.Equal(t, 7*time.Minute, newRun(m, Options{}).timeoutFor(r))

		m.SetResourceTimeout(time.Minute)
		assert.Equal(t, 7*time.Minute, newRun(m, Options{}).timeoutFor(r), "the resource's own timeout beats the manifest's")

		m.WithTimeout(r, 2*time.Minute)
		assert.Equal(t, 2*time.Minute, newRun(m, Options{}).timeoutFor(r), "WithTimeout beats the resource")
	})

	t.Run("a negative params timeout means none", func(t *testing.T) {
		slow := &paramsTimeoutResource{timeout: -1}
		slow.block = make(chan struct{})

		m := New()
		m.Add(slow)
		m.SetResourceTimeout(10 * time.Millisecond)

		go func() {
			time.Sleep(100 * time.Millisecond)
			slow.release()
		}()

		report, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)
		assert.Equal(t, StatusSuccess, report.Results[0].Status)
	})
}

// composite uses other resources once it knows what they need
type composite struct {
	testResourceType
	path string
}

func (c *composite) Run(ctx context.Context, log *Logger) error {
	return ApplyOne(ctx, &writeResource{path: c.path}, log)
}

// writeResource writes a file under the root
type writeResource struct {
	testResourceType
	path      string
	preflight bool
}

func (w *writeResource) PreflightChecks(ctx context.Context, log *Logger) error {
	if w.path == "" {
		return errors.New("required parameter: path")
	}

	w.preflight = true

	return nil
}

func (w *writeResource) Run(ctx context.Context, log *Logger) error {
	env := EnvFrom(ctx)

	if env.DryRun {
		log.Info("would write", "path", w.path)
		return nil
	}

	log.Info("wrote", "path", w.path)

	return os.WriteFile(env.Path(w.path), []byte("x"), 0o644)
}

func TestApplyOne(t *testing.T) {
	t.Run("runs preflight then run with the parent's logger and env", func(t *testing.T) {
		root := t.TempDir()

		m := New()
		r := m.Add(&composite{path: "/file"})

		report, err := m.Apply(context.Background(), Options{Env: Env{Root: root, Runner: &countingRunner{}}})
		require.NoError(t, err)

		assert.FileExists(t, filepath.Join(root, "file"))
		require.Len(t, report.Results, 1)
		assert.Equal(t, r.ResourceID, report.Results[0].ID)
		assert.True(t, report.Results[0].Changed, "changes by the inner resource count")
	})

	t.Run("preflight errors are returned", func(t *testing.T) {
		err := ApplyOne(context.Background(), &writeResource{}, NewLogger(nil, "Test", "Test"))
		assert.ErrorContains(t, err, "required parameter: path")
	})

	t.Run("dry run", func(t *testing.T) {
		root := t.TempDir()
		ctx := WithEnv(context.Background(), Env{Root: root, DryRun: true})

		w := &writeResource{path: "/file"}
		log := NewLogger(nil, "Test", "Test")
		require.NoError(t, ApplyOne(ctx, w, log))

		assert.True(t, w.preflight)
		assert.True(t, log.Changed())
		assert.NoFileExists(t, filepath.Join(root, "file"))
	})
}
