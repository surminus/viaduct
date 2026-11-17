package viaduct

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ExpandPath expands a leading "~" to the home directory of the user
// attribute.
func ExpandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		return filepath.Join(Attributes().User.HomeDir, strings.TrimPrefix(path, "~"))
	}

	return path
}

// Must returns v, panicking if err is not nil. It is for values known at
// build time, such as an embedded template, where an error is a bug.
func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

// IsUbuntu returns true if the distribution is Ubuntu
func IsUbuntu() bool {
	// Linux Mint uses IDLike since it's an Ubuntu fork
	return strings.Contains(Attributes().Platform.ID, "ubuntu") || strings.Contains(Attributes().Platform.IDLike, "ubuntu")
}

// FileExists returns true if the file exists
func FileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}

	return false
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

// ListFiles returns every path within a path, including the path itself.
// Paths removed during the walk are skipped.
func ListFiles(path string) (files []string, err error) {
	err = filepath.Walk(path, func(p string, f os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p != path {
				return nil
			}

			return err
		}

		files = append(files, p)
		return nil
	})

	return files, err
}

// MatchChmod reports whether the mode of the path matches perms.
func MatchChmod(path string, perms fs.FileMode) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	return info.Mode() == perms, nil
}

// MatchChown reports whether the path is owned by the given user and group.
func MatchChown(path string, user, group int) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("cannot read ownership of %s", path)
	}

	return user == int(stat.Uid) && group == int(stat.Gid), nil
}

// IsRoot returns true if the user is root
func IsRoot() bool {
	return Attributes().runuser.Username == "root"
}

// FileSize returns the file size in bytes
func FileSize(path string) (int64, error) {
	f, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	return f.Size(), nil
}
