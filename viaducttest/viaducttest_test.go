package viaducttest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/surminus/viaduct"
)

// fakeTB records the errors a Runner reports, rather than failing the test
type fakeTB struct {
	testing.TB
	errors []string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Errorf(format string, args ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
}

func run(t *testing.T, r *Runner, name string, args ...string) (string, error) {
	t.Helper()

	ctx := viaduct.WithEnv(context.Background(), viaduct.Env{Runner: r})

	out, err := viaduct.Command{Name: name, Args: args}.Output(ctx, nil)

	return string(out), err
}

func TestRunner(t *testing.T) {
	t.Run("an unexpected command fails the test", func(t *testing.T) {
		tb := &fakeTB{}
		r := NewRunner(tb)
		r.On("apt-get", "update")

		_, err := run(t, r, "apt-get", "install", "curl")
		require.Error(t, err)

		require.Len(t, tb.errors, 1)
		assert.Contains(t, tb.errors[0], "unexpected command: apt-get install curl")
		assert.Equal(t, []string{"apt-get install curl"}, r.Commands(), "the call is still recorded")
	})

	t.Run("output and exit status", func(t *testing.T) {
		r := NewRunner(t)
		r.On("dpkg", "-s", "curl").Stdout("installed\n").Exit(1)

		out, err := run(t, r, "dpkg", "-s", "curl")
		assert.Equal(t, "installed\n", out)

		code, ok := viaduct.ExitCode(err)
		assert.True(t, ok)
		assert.Equal(t, 1, code)
	})

	t.Run("Fail", func(t *testing.T) {
		r := NewRunner(t)
		boom := errors.New("no such program")
		r.OnAny("missing").Fail(boom)

		_, err := run(t, r, "missing", "anything")
		assert.ErrorIs(t, err, boom)

		_, ok := viaduct.ExitCode(err)
		assert.False(t, ok)
	})

	t.Run("an error from Do is the command's error", func(t *testing.T) {
		r := NewRunner(t)
		boom := errors.New("boom")

		var got viaduct.Command
		r.On("touch", "/x").Do(func(c viaduct.Command) error {
			got = c
			return boom
		})

		_, err := run(t, r, "touch", "/x")
		assert.ErrorIs(t, err, boom)
		assert.Equal(t, "touch /x", got.String())
	})

	t.Run("later responses override earlier ones", func(t *testing.T) {
		r := NewRunner(t)
		r.OnAny("systemctl").Stdout("any\n")
		r.On("systemctl", "is-active", "nginx").Stdout("active\n")

		out, err := run(t, r, "systemctl", "is-active", "nginx")
		require.NoError(t, err)
		assert.Equal(t, "active\n", out)

		out, err = run(t, r, "systemctl", "is-enabled", "nginx")
		require.NoError(t, err)
		assert.Equal(t, "any\n", out)

		r.On("systemctl", "is-active", "nginx").Exit(3)

		_, err = run(t, r, "systemctl", "is-active", "nginx")
		code, _ := viaduct.ExitCode(err)
		assert.Equal(t, 3, code)
	})

	t.Run("Ran", func(t *testing.T) {
		r := NewRunner(t)
		r.OnAny("echo")

		_, err := run(t, r, "echo", "hello", "world")
		require.NoError(t, err)

		assert.True(t, r.Ran("echo", "hello", "world"))
		assert.False(t, r.Ran("echo", "hello"))
		assert.False(t, r.Ran("printf", "hello", "world"))
	})

	t.Run("a cancelled context stops the command", func(t *testing.T) {
		r := NewRunner(t)
		r.OnAny("sleep")

		ctx, cancel := context.WithCancel(viaduct.WithEnv(context.Background(), viaduct.Env{Runner: r}))
		cancel()

		err := viaduct.Command{Name: "sleep", Args: []string{"1"}}.Run(ctx, nil)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestEnv(t *testing.T) {
	t.Run("context", func(t *testing.T) {
		e := NewEnv(t)

		env := viaduct.EnvFrom(e.Context())
		assert.Equal(t, e.Root, env.Root)
		assert.Same(t, e.Runner, env.Runner)
		assert.False(t, env.DryRun)

		e.DryRun = true
		assert.True(t, viaduct.EnvFrom(e.Context()).DryRun)

		e.DryRun = false
		assert.True(t, viaduct.EnvFrom(e.DryRunContext()).DryRun)
	})

	t.Run("paths stay under the root", func(t *testing.T) {
		e := NewEnv(t)

		assert.Equal(t, e.Root+"/etc/passwd", e.Path("/../etc/passwd"))

		e.WriteFile("/etc/hosts", "127.0.0.1 localhost\n")
		assert.Equal(t, "127.0.0.1 localhost\n", e.ReadFile("/etc/hosts"))
	})

	t.Run("Apply uses the environment's context when given none", func(t *testing.T) {
		e := NewEnv(t)
		e.DryRun = true

		r := &envResource{}

		//nolint:staticcheck // a nil context is what is being tested
		_, err := e.Apply(nil, r)
		require.NoError(t, err)

		assert.True(t, r.env.DryRun)
		assert.Equal(t, e.Root, r.env.Root)
	})
}

// envResource records the environment it ran in
type envResource struct {
	env viaduct.Env
}

func (r *envResource) Description() string                                    { return "env" }
func (r *envResource) OperationName() string                                  { return "Test" }
func (r *envResource) Params() *viaduct.ResourceParams                        { return viaduct.NewResourceParams() }
func (r *envResource) PreflightChecks(context.Context, *viaduct.Logger) error { return nil }

func (r *envResource) Run(ctx context.Context, _ *viaduct.Logger) error {
	r.env = viaduct.EnvFrom(ctx)
	return nil
}
