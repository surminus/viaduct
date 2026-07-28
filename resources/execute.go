package resources

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/surminus/viaduct"
)

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

	// Unless is another command to run, which if exits cleanly signifies
	// that we should not run the execute command. It runs through bash in the
	// same way as Command. Optional.
	Unless string

	// Lock ensures the command does not run at the same time as other
	// resources holding a lock, such as Package. Useful for commands that
	// need the dpkg lock, like apt-get or dpkg. Optional.
	Lock bool

	// LockKey narrows the lock to a single domain, such as
	// viaduct.PackageLock, so the command only waits for other resources
	// using the same key. Implies Lock. Optional.
	LockKey string
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
	return &viaduct.ResourceParams{GlobalLock: e.Lock || e.LockKey != "", LockKey: e.LockKey}
}

func (e *Execute) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if e.Command == "" && len(e.Args) == 0 {
		return fmt.Errorf("required parameter: Command")
	}

	if e.Command != "" && len(e.Args) > 0 {
		return fmt.Errorf("cannot set both Command and Args")
	}

	// Set optional defaults here
	return nil
}

func (e *Execute) OperationName() string {
	return "Run"
}

func (e *Execute) Run(log *viaduct.Logger) error {
	return e.runExecute(log)
}

// Run runs the given command
func (e *Execute) runExecute(log *viaduct.Logger) error {
	if e.Unless != "" {
		// nolint:gosec
		ucmd := exec.Command("bash", "-c", e.Unless)
		setCommandOutput(ucmd)

		if err := ucmd.Run(); err == nil {
			log.Noop("skipped", "command", e.Description())
			return nil
		}
	}

	log.Info("started", "command", e.Description())
	if viaduct.Cli.DryRun {
		return nil
	}

	cmd := e.command()
	setCommandOutput(cmd)
	cmd.Dir = e.WorkingDirectory

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command failed: %s", e.Description())
	}
	log.Info("finished", "command", e.Description())

	return nil
}

// command builds the command to run, using a shell only when the command was
// given as a single string
func (e *Execute) command() *exec.Cmd {
	if len(e.Args) > 0 {
		// nolint:gosec
		return exec.Command(e.Args[0], e.Args[1:]...)
	}

	// nolint:gosec
	return exec.Command("bash", "-c", e.Command)
}

func setCommandOutput(cmd *exec.Cmd) {
	if viaduct.Cli.Silent || viaduct.Cli.JSON {
		cmd.Stdout = nil
		cmd.Stderr = nil
		return
	}

	if viaduct.Cli.Quiet {
		cmd.Stdout = nil
		cmd.Stderr = os.Stderr
		return
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
}
