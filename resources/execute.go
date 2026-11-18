package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/surminus/viaduct"
)

// Execute runs a command. It reports a change on every run, unless Unless
// says there is nothing to do.
type Execute struct {
	// Command is the command to run. It is passed to "bash -c", so shell
	// syntax such as pipes and redirection works, and any value containing
	// spaces or shell metacharacters has to be quoted by the caller. Use Args
	// when you want arguments passed to the program verbatim.
	Command string

	// Args runs a command without a shell: Args[0] is the program, and the
	// rest are passed as literal arguments, so values containing spaces or
	// shell metacharacters need no quoting. Set either Command or Args.
	Args []string

	// WorkingDirectory is where to run the command. Optional.
	WorkingDirectory string

	// User runs the command as this user, with runuser, which sets HOME,
	// USER and LOGNAME for them. Requires root. Optional.
	User string

	// Env is added to the command's environment, as "KEY=value". Optional.
	Env []string

	// Unless is another command to run, which if exits cleanly signifies
	// that we should not run the execute command. It runs through bash in the
	// same way as Command, in WorkingDirectory, as User and with Env.
	// Optional.
	Unless string

	// Lock ensures the command does not run at the same time as other
	// resources holding a lock, such as Package. Useful for commands that
	// need the dpkg lock, like apt-get or dpkg. Optional.
	Lock bool

	// LockKey narrows the lock to a single domain, such as
	// viaduct.PackageLock, so the command only waits for other resources
	// using the same key. Implies Lock. With viaduct.PackageLock the command
	// has no timeout by default. Optional.
	LockKey viaduct.LockKey
}

// Exec is a shortcut for running a command
func Exec(command string) *Execute {
	return &Execute{Command: command}
}

// ExecArgs is like Exec, but takes the program and its arguments separately
// and runs them without a shell, so no quoting or escaping is needed
func ExecArgs(args ...string) *Execute {
	return &Execute{Args: args}
}

// ExecUnless is like Exec, but will only run conditionally
func ExecUnless(command, unless string) *Execute {
	return &Execute{Command: command, Unless: unless}
}

// ExecLocked is like Exec, but takes the global lock
func ExecLocked(command string) *Execute {
	return &Execute{Command: command, Lock: true}
}

// ExecArgsLocked is like ExecArgs, but takes the global lock
func ExecArgsLocked(args ...string) *Execute {
	return &Execute{Args: args, Lock: true}
}

func Echo(message string) *Execute {
	return &Execute{Command: fmt.Sprintf("echo \"%s\"", message)}
}

func (e *Execute) Description() string {
	if len(e.Args) > 0 {
		return strings.Join(e.Args, " ")
	}

	return e.Command
}

func (e *Execute) Params() *viaduct.ResourceParams {
	return lockParams(e.Lock, e.LockKey)
}

// lockParams returns the params of a command resource. One holding the
// package lock gets no timeout, as Package does, since stopping dpkg part way
// through leaves it interrupted.
func lockParams(lock bool, key viaduct.LockKey) *viaduct.ResourceParams {
	params := &viaduct.ResourceParams{GlobalLock: lock || key != "", LockKey: key}

	if key == viaduct.PackageLock {
		params.Timeout = noTimeout
	}

	return params
}

func (e *Execute) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if e.Command == "" && len(e.Args) == 0 {
		return fmt.Errorf("required parameter: Command")
	}

	if e.Command != "" && len(e.Args) > 0 {
		return fmt.Errorf("cannot set both Command and Args")
	}

	if e.User != "" {
		if err := requireRoot(ctx, "execute"); err != nil {
			return fmt.Errorf("%w to run as another user", err)
		}
	}

	// Set optional defaults here
	return nil
}

func (e *Execute) OperationName() string {
	return "Run"
}

func (e *Execute) Run(ctx context.Context, log *viaduct.Logger) error {
	if unlessSucceeds(ctx, log, e.Unless, e.prepare) {
		log.Noop("skipped", "command", e.Description())
		return nil
	}

	if wouldRun(ctx, log, "command", e.Description()) {
		return nil
	}

	c := e.prepare(e.command())

	if err := c.Run(ctx, log); err != nil {
		return fmt.Errorf("command failed: %s: %w", e.Description(), err)
	}
	log.Info("finished", "command", e.Description())

	return nil
}

// wouldRun logs, in a dry run, the command or script that would run, and
// reports whether it is a dry run. A real run logs nothing until the command
// has finished, so one that fails does not mark the resource changed.
func wouldRun(ctx context.Context, log *viaduct.Logger, kind, description string) bool {
	if !viaduct.EnvFrom(ctx).DryRun {
		return false
	}

	log.Info("started", kind, description)

	return true
}

// prepare sets up a command to run in the working directory with Env, as
// User
func (e *Execute) prepare(c viaduct.Command) viaduct.Command {
	c.Dir = e.WorkingDirectory
	c.Env = e.Env

	if e.User != "" {
		c.Args = append([]string{"-u", e.User, "--", c.Name}, c.Args...)
		c.Name = "runuser"
	}

	return c
}

// command builds the command to run, using a shell only when the command was
// given as a single string
func (e *Execute) command() viaduct.Command {
	if len(e.Args) > 0 {
		return viaduct.Command{Name: e.Args[0], Args: e.Args[1:]}
	}

	return viaduct.Command{Name: "bash", Args: []string{"-c", e.Command}}
}

// unlessSucceeds runs an Unless guard through bash, set up by prepare as the
// resource's own command is, reporting whether it exited cleanly. An empty
// guard never succeeds, so the resource runs. The guard shares the
// resource's working directory, user and environment so it checks the same
// thing the resource acts on. It runs in a dry run too.
func unlessSucceeds(ctx context.Context, log *viaduct.Logger, unless string, prepare func(viaduct.Command) viaduct.Command) bool {
	if unless == "" {
		return false
	}

	c := prepare(viaduct.Command{Name: "bash", Args: []string{"-c", unless}})

	return c.Run(ctx, log) == nil
}
