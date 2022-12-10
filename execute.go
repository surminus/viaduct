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

// E is a shortcut for declaring a new Execute resource
func E(command string) Execute {
	return Execute{Command: command}
}

func (e *Execute) satisfy(log *logger) error {
	// Set required values here, and error if they are not set
	if e.Command == "" {
		return fmt.Errorf("Required parameter: Command")
	}

	// Set optional defaults here
	return nil
}

// Run can be used in scripting mode to run a command
func (e Execute) Run() *Execute {
	log := newLogger("Execute", "run")
	err := e.runExecute(log)
	if err != nil {
		log.Fatal(err)
	}

	return &e
}

// Run runs the given command
func (e Execute) runExecute(log *logger) error {
	if err := e.satisfy(log); err != nil {
		return err
	}

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
