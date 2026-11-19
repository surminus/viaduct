package viaduct

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Command is a program for a Runner to run.
type Command struct {
	// Name is the program to run, looked up in PATH if it has no slash.
	Name string

	// Args are passed to the program as they are, without a shell.
	Args []string

	// Dir is the working directory. Empty means the current one.
	Dir string

	// Env is added to the environment, as "KEY=value". Optional.
	Env []string

	// Stdin is the program's input. Nil means none.
	Stdin io.Reader

	// Stdout and Stderr receive the program's output. Nil discards it, but
	// Run and Output log it instead.
	Stdout, Stderr io.Writer
}

// String returns the command line, for logs and errors.
func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Run runs the command with the context's Runner. Output not sent elsewhere is
// logged a line at a time.
func (c Command) Run(ctx context.Context, log *Logger) error {
	if c.Stdout == nil {
		w := log.Writer(Stdout)
		defer w.Close()

		c.Stdout = w
	}

	if c.Stderr == nil {
		w := log.Writer(Stderr)
		defer w.Close()

		c.Stderr = w
	}

	return EnvFrom(ctx).Runner.Run(ctx, c)
}

// Output runs the command like Run, returning its standard output rather
// than logging it.
func (c Command) Output(ctx context.Context, log *Logger) ([]byte, error) {
	var out bytes.Buffer
	c.Stdout = &out

	err := c.Run(ctx, log)

	return out.Bytes(), err
}

// Runner runs commands. Env.Runner replaces it, to test resources without
// running anything.
type Runner interface {
	// Run runs the command to completion, returning an error that ExitCode
	// understands when it exits with a non-zero status. Cancelling the
	// context stops it.
	Run(ctx context.Context, c Command) error
}

// RunnerFunc adapts a function to a Runner.
type RunnerFunc func(ctx context.Context, c Command) error

// Run calls f.
func (f RunnerFunc) Run(ctx context.Context, c Command) error {
	return f(ctx, c)
}

// ExitError is a command exiting with a non-zero status.
type ExitError struct {
	Code int
}

// Error returns "exit status" and the code.
func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

// ExitCode returns the exit status.
func (e *ExitError) ExitCode() int {
	return e.Code
}

// ExitCode returns the exit status of a command that failed, and whether err
// came from a command exiting at all.
func ExitCode(err error) (int, bool) {
	var e interface{ ExitCode() int }
	if errors.As(err, &e) {
		return e.ExitCode(), true
	}

	return 0, false
}

// commandWaitDelay bounds the wait for a command's output to close after it
// exits. A background process can hold the output open indefinitely.
const commandWaitDelay = 2 * time.Second

// commandCancelGrace is how long a command has to stop after SIGTERM before
// its process group is killed. Package managers can take a while to stop
// cleanly, and killing one part way through can leave its database locked.
var commandCancelGrace = 30 * time.Second

// ExecRunner runs commands on the host. Each command runs in its own process
// group. When the context is cancelled the group is sent SIGTERM, and SIGKILL
// if the command has not stopped 30 seconds later. Output from a background
// process left running is collected for two seconds after the command exits.
var ExecRunner Runner = execRunner{}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, c Command) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// The context is handled below rather than by exec.CommandContext, which
	// uses one delay both for output after exit and for the kill after
	// cancelling.
	// nolint:gosec
	cmd := exec.Command(c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	cmd.Stdout = c.Stdout
	cmd.Stderr = c.Stderr

	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}

	// Own process group, so children are stopped with the command
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = commandWaitDelay

	if err := cmd.Start(); err != nil {
		return err
	}

	exited := make(chan struct{})
	stopped := make(chan struct{})

	var signalled bool

	go func(pid int, grace time.Duration) {
		defer close(stopped)
		signalled = stopOnCancel(ctx, pid, grace, exited)
	}(cmd.Process.Pid, commandCancelGrace)

	err := cmd.Wait()

	close(exited)
	<-stopped

	// The command has gone, but children that ignored SIGTERM may not have
	if ctx.Err() != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	// A command stopped part way is not a success, even one that exits 0 on
	// SIGTERM
	if signalled {
		if err == nil || errors.Is(err, exec.ErrWaitDelay) {
			return ctx.Err()
		}

		return fmt.Errorf("%w: %w", ctx.Err(), err)
	}

	// The command succeeded; only a leftover process holds the output open
	if errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}

	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("%w: %w", ctx.Err(), err)
	}

	return err
}

// stopOnCancel sends SIGTERM to the process group once ctx is cancelled, and
// SIGKILL if the command has not exited after grace. It reports whether it
// sent anything.
func stopOnCancel(ctx context.Context, pid int, grace time.Duration, exited <-chan struct{}) bool {
	select {
	case <-exited:
		return false
	case <-ctx.Done():
	}

	_ = syscall.Kill(-pid, syscall.SIGTERM)

	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case <-exited:
	case <-timer.C:
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}

	return true
}
