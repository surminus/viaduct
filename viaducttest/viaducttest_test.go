package viaducttest

import (
	"context"
	"errors"
	"fmt"
	"os"
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

	t.Run("Times limits a response, falling back to an earlier one", func(t *testing.T) {
		r := NewRunner(t)
		r.On("blkid", "/dev/sdb").Stdout("ext4\n")
		r.On("blkid", "/dev/sdb").Exit(2).Times(1)

		_, err := run(t, r, "blkid", "/dev/sdb")
		code, _ := viaduct.ExitCode(err)
		assert.Equal(t, 2, code)

		out, err := run(t, r, "blkid", "/dev/sdb")
		require.NoError(t, err)
		assert.Equal(t, "ext4\n", out)

		out, err = run(t, r, "blkid", "/dev/sdb")
		require.NoError(t, err)
		assert.Equal(t, "ext4\n", out)
	})

	t.Run("Then answers in the order declared", func(t *testing.T) {
		r := NewRunner(t)
		r.On("blkid", "/dev/sdb").Exit(2).Then().Stdout("xfs\n").Times(2).Then().Stdout("ext4\n")

		_, err := run(t, r, "blkid", "/dev/sdb")
		code, _ := viaduct.ExitCode(err)
		assert.Equal(t, 2, code)

		for _, want := range []string{"xfs\n", "xfs\n", "ext4\n", "ext4\n"} {
			out, err := run(t, r, "blkid", "/dev/sdb")
			require.NoError(t, err)
			assert.Equal(t, want, out)
		}
	})

	t.Run("Then falls back to an earlier response once its chain is used up", func(t *testing.T) {
		r := NewRunner(t)
		r.On("blkid", "/dev/sdb").Stdout("ext4\n")
		r.On("blkid", "/dev/sdb").Exit(2).Then().Stdout("xfs\n").Times(1)

		_, err := run(t, r, "blkid", "/dev/sdb")
		require.Error(t, err)

		for _, want := range []string{"xfs\n", "ext4\n", "ext4\n"} {
			out, err := run(t, r, "blkid", "/dev/sdb")
			require.NoError(t, err)
			assert.Equal(t, want, out)
		}
	})

	t.Run("Then keeps to its own command", func(t *testing.T) {
		r := NewRunner(t)
		r.OnAny("blkid").Exit(2).Then().Stdout("xfs\n")
		r.On("mkfs", "/dev/sdb")

		_, err := run(t, r, "blkid", "/dev/sdb")
		require.Error(t, err)

		_, err = run(t, r, "mkfs", "/dev/sdb")
		require.NoError(t, err)

		out, err := run(t, r, "blkid", "/dev/sdc")
		require.NoError(t, err)
		assert.Equal(t, "xfs\n", out)
	})

	t.Run("a used up response fails the test without an earlier one", func(t *testing.T) {
		tb := &fakeTB{}
		r := NewRunner(tb)
		r.On("true").Times(1)

		_, err := run(t, r, "true")
		require.NoError(t, err)

		_, err = run(t, r, "true")
		require.Error(t, err)
		assert.Len(t, tb.errors, 1)
	})

	t.Run("Otherwise answers commands nothing else matches", func(t *testing.T) {
		r := NewRunner(t)
		r.Otherwise().Stdout("fallback\n")
		r.On("systemctl", "is-active", "nginx").Exit(3)

		out, err := run(t, r, "dpkg-query", "-W", "curl")
		require.NoError(t, err)
		assert.Equal(t, "fallback\n", out)

		_, err = run(t, r, "systemctl", "is-active", "nginx")
		code, _ := viaduct.ExitCode(err)
		assert.Equal(t, 3, code)
	})

	t.Run("Exit is an exit status from Do", func(t *testing.T) {
		r := NewRunner(t)
		r.On("false").Do(func(viaduct.Command) error { return Exit(4) })

		_, err := run(t, r, "false")
		code, ok := viaduct.ExitCode(err)
		assert.True(t, ok)
		assert.Equal(t, 4, code)
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

	t.Run("a path can be marked as a block device", func(t *testing.T) {
		e := NewEnv(t)
		e.BlockDevice("/dev/sdb")
		e.WriteFile("/dev/sdc", "")

		env := viaduct.EnvFrom(e.Context())

		block, err := env.IsBlockDevice("/dev/sdb")
		require.NoError(t, err)
		assert.True(t, block)

		block, err = env.IsBlockDevice("/dev/sdc")
		require.NoError(t, err)
		assert.False(t, block, "a file that was not marked")

		_, err = env.IsBlockDevice("/dev/sdd")
		assert.ErrorIs(t, err, os.ErrNotExist)
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
