package viaduct

import (
	"os"
	"os/exec"
	"strings"
)

type Execute struct {
	// Command is the command to run
	Command string
	// WorkingDirectory is where to run the command
	WorkingDirectory string
	// Unless is another command to run, which if exits cleanly signifies
	// that we should not run the execute command
	Unless string
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

	log.Info(e.Command)
	if Config.DryRun {
		return &e
	}

	if e.Unless != "" {
		log.Warn("Unless: ", e.Unless)

		unless := strings.Split(e.Unless, " ")
		// nolint:gosec
		ucmd := exec.Command(unless[0], unless[1:]...)
		ucmd.Stdout = os.Stdout
		ucmd.Stderr = os.Stderr

		if err := ucmd.Run(); err == nil {
			return &e
		}
	}

	command := strings.Split(e.Command, " ")

	// nolint:gosec
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = e.WorkingDirectory

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}

	return &e
}
