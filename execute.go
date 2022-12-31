package viaduct

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Execute struct {
	// Command is the command to run
	Command string

	// WorkingDirectory is where to run the command. Optional.
	WorkingDirectory string

	// Unless is another command to run, which if exits cleanly signifies
	// that we should not run the execute command. Optional.
	Unless string

	// Quiet suppresses output from STDOUT. Optional.
	Quiet bool
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

func (e *Execute) opts() *ResourceOptions {
	return NewResourceOptions()
}

func (e *Execute) satisfy(log *logger) error {
	// Set required values here, and error if they are not set
	if e.Command == "" {
		return fmt.Errorf("Required parameter: Command")
	}

	// Set optional defaults here
	return nil
}

func (e *Execute) operationName() string {
	return "Run"
}

func (e *Execute) run(log *logger) error {
	return e.runExecute(log)
}

// Run runs the given command
func (e *Execute) runExecute(log *logger) error {
	if e.Unless != "" {
		unless := strings.Split(e.Unless, " ")

		// nolint:gosec
		ucmd := exec.Command("bash", "-c", strings.Join(unless, " "))
		ucmd.Stdout = os.Stdout
		ucmd.Stderr = os.Stderr

		if err := ucmd.Run(); err == nil {
			log.Noop(e.Command)
			return nil
		}
	}

	log.Info(e.Command, " -> started")
	if Config.DryRun {
		return nil
	}

	command := strings.Split(e.Command, " ")

	// nolint:gosec
	cmd := exec.Command("bash", "-c", strings.Join(command, " "))
	if !e.Quiet {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	cmd.Dir = e.WorkingDirectory

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command failed: %s", e.Command)
	}
	log.Info(e.Command, " -> finished")

	return nil
}
