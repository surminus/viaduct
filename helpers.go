package viaduct

import (
	"log"
	"os"
	"os/exec"
	"strings"

	homedir "github.com/mitchellh/go-homedir"
)

// ExpandPath ensures that "~" are expanded.
func ExpandPath(path string) string {
	p, err := homedir.Expand(path)
	if err != nil {
		log.Fatal(err)
	}

	return p
}

// PrependSudo takes a slice of args and simply prepends sudo to the front.
func PrependSudo(args []string) []string {
	return append([]string{"sudo"}, args...)
}

// RunCommand is essentially a wrapper around exec.Command. Generally the
// Execute resource should be used, but sometimes it can be useful to run
// things directly.
func RunCommand(command ...string) error {
	cmd := exec.Command("bash", "-c", strings.Join(command, " "))
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// SudoCommand is the same as RunCommand but runs with sudo.
func SudoCommand(command ...string) error {
	return RunCommand(PrependSudo(command)...)
}

// IsUbuntu returns true if the distribution is Ubuntu
func IsUbuntu() bool {
	return strings.Contains(Attribute.Platform.IDLike, "ubuntu")
}

// FileExists returns true if the file exists
func FileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}

	return false
}
