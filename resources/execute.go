package resources

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/surminus/viaduct"
)

type Execute struct {
	// Command is the command to run
	Command string

	// WorkingDirectory is where to run the command. Optional.
	WorkingDirectory string

	// Unless is another command to run, which if exits cleanly signifies
	// that we should not run the execute command. Optional.
	Unless string
}

// Exec is a shortcut for running a command
func Exec(command string) *Execute {
	return &Execute{Command: command}
}

// ExecUnless is like Exec, but will only run conditionally
func ExecUnless(command, unless string) *Execute {
	return &Execute{Command: command, Unless: unless}
}

func Echo(message string) *Execute {
	return &Execute{Command: fmt.Sprintf("echo \"%s\"", message)}
}

func (e *Execute) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

func (e *Execute) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if e.Command == "" {
		return fmt.Errorf("Required parameter: Command")
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
		unless := strings.Split(e.Unless, " ")

		// nolint:gosec
		ucmd := exec.Command("bash", "-c", strings.Join(unless, " "))
		setCommandOutput(ucmd)

		if err := ucmd.Run(); err == nil {
			log.Noop(e.Command)
			return nil
		}
	}

	log.Info(e.Command, " -> started")
	if viaduct.Cli.DryRun {
		return nil
	}

	command := strings.Split(e.Command, " ")

	// nolint:gosec
	cmd := exec.Command("bash", "-c", strings.Join(command, " "))
	setCommandOutput(cmd)
	cmd.Dir = e.WorkingDirectory

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command failed: %s", e.Command)
	}
	log.Info(e.Command, " -> finished")

	return nil
}

func setCommandOutput(cmd *exec.Cmd) {
	if viaduct.Cli.Silent {
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
