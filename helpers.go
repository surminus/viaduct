package viaduct

import (
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"syscall"

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

// SetDefaultUser allows us to assign a default username
func SetDefaultUser(username string) {
	u, err := user.Lookup(username)
	if err != nil {
		log.Fatal(err)
	}

	Attribute.User = *u
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

// DirExists returns true if the file exists, and is a directory
func DirExists(path string) bool {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return true
		}
	}

	return false
}

// MatchChmod returns true if the permissions of the path match
func MatchChmod(path string, perms fs.FileMode) bool {
	if info, err := os.Stat(path); err == nil {
		if info.Mode() == perms {
			return true
		}

		log.Print(info.Mode().String())
	} else {
		log.Fatal(err)
	}

	return false
}

// MatchChown returns true if the path is owned by the specified user and group
func MatchChown(path string, user, group int) bool {
	if info, err := os.Stat(path); err == nil {
		stat := info.Sys().(*syscall.Stat_t)

		uid := stat.Uid
		gid := stat.Gid

		if user == int(uid) && group == int(gid) {
			return true
		}
	} else {
		log.Fatal(err)
	}

	return false
}
