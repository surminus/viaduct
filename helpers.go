package viaduct

import (
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// ExpandPath ensures that "~" are expanded.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		if p, err := filepath.Abs(strings.Replace(path, "~", Attribute.User.HomeDir, 1)); err == nil {
			path = p
		} else {
			log.Fatal(err)
		}
	}

	return path
}

// ExpandPathRoot is like ExpandPath, but ignores the user attribute
func ExpandPathRoot(path string) string {
	p, err := filepath.Abs(path)
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

// FileContents simply returns the file contents as a string
func FileContents(path string) string {
	c, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}

	return string(c)
}

// LinkExists returns true if the symlink exists
func LinkExists(path string) bool {
	if _, err := os.Lstat(path); err == nil {
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

// IsRoot returns true if the user is root
func IsRoot() bool {
	return Attribute.runuser.Username == "root"
}

// TmpFile returns the path for a Viaduct temporary file
func TmpFile(path string) string {
	return filepath.Join(Attribute.TmpDir, path)
}

// FileSize returns the file size in bytes
func FileSize(path string) int64 {
	f, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
	}

	return f.Size()
}

// CommandTrue is similar to the "Unless" parameter found in some resources, but instead
// can be used freeform within configuration. If it exits cleanly, then it
// returns true.
func CommandTrue(command string) bool {
	c := strings.Split(command, " ")

	// nolint:gosec
	cmd := exec.Command("bash", "-c", strings.Join(c, " "))
	if Config.Quiet {
		cmd.Stderr = os.Stderr
	} else if !Config.Silent {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err == nil {
		return true
	} else {
		return false
	}
}

// CommandFalse is the same as True, but returns the opposite.
func CommandFalse(command string) bool {
	return !CommandTrue(command)
}

// CommandOutput will run a command and provide the output. Can be useful in if
// statements for checking arbitary values. Will not error, but will return an
// empty string.
func CommandOutput(command string) string {
	c := strings.Split(command, " ")
	cmd := exec.Command("bash", "-c", strings.Join(c, " "))

	if out, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(out))
	} else {
		return ""
	}
}
