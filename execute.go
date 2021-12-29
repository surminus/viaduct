package viaduct

import (
	"log"
	"os"
	"os/exec"
	"strings"
)

type Execute struct {
	Command          string
	WorkingDirectory string
}

func (e *Execute) satisfy() {
	// Set required values here, and error if they are not set
	if e.Command == "" {
		log.Fatal("==> Execute [error] Required parameter: Command")
	}

	// Set optional defaults here
}

// Run runs the given command
func (e Execute) Run() *Execute {
	e.satisfy()

	log.Println("==> Execute [run]", e.Command)
	if Config.DryRun {
		return &e
	}

	command := strings.Split(e.Command, " ")

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = e.WorkingDirectory

	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}

	return &e
}
