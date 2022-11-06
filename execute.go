package viaduct

import (
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

	// Sudo runs the command using sudo. Optional.
	Sudo bool

	// Quiet suppresses output from STDOUT. Optional.
	Quiet bool
}

func (e *Execute) satisfy(log *logger) {
	// Set required values here, and error if they are not set
	if e.Command == "" {
		log.Fatal("Required parameter: Command")
	}

	// Set optional defaults here
}

// Run runs the given command
func (e Execute) Run() *Execute {
	log := newLogger("Execute", "run")
	e.satisfy(log)

	if e.Unless != "" {
		unlessLog := newLogger("Execute", "unless")
		unless := strings.Split(e.Unless, " ")
		if e.Sudo {
			unless = PrependSudo(unless)
		}

		// nolint:gosec
		ucmd := exec.Command("bash", "-c", strings.Join(unless, " "))
		ucmd.Stdout = os.Stdout
		ucmd.Stderr = os.Stderr

		if err := ucmd.Run(); err == nil {
			unlessLog.Info(e.Unless)
			log.Noop(e.Command)
			return &e
		}

		unlessLog.Warn(e.Unless)
	}

	log.Info(e.Command)
	if Config.DryRun {
		return &e
	}

	command := strings.Split(e.Command, " ")
	if e.Sudo {
		command = PrependSudo(command)
	}

	// nolint:gosec
	cmd := exec.Command("bash", "-c", strings.Join(command, " "))
	if !e.Quiet {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	cmd.Dir = e.WorkingDirectory

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}

	return &e
}
