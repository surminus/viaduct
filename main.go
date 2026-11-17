package viaduct

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// tmp holds the run's temporary directory. It is created on first use, so
// importing viaduct leaves nothing behind.
var tmp struct {
	sync.Mutex
	dir string

	// applies counts the Apply calls running, which share the directory.
	applies int
}

// TmpDir returns a private temporary directory, creating it on first use. It
// is removed once no Apply is running.
func TmpDir() (string, error) {
	tmp.Lock()
	defer tmp.Unlock()

	if tmp.dir != "" {
		return tmp.dir, nil
	}

	dir, err := os.MkdirTemp("", "viaduct-")
	if err != nil {
		return "", fmt.Errorf("creating temporary directory: %w", err)
	}

	tmp.dir = dir

	return dir, nil
}

// TmpFile returns the path for a file in the run's temporary directory,
// creating the directory on first use.
func TmpFile(name string) (string, error) {
	dir, err := TmpDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, name), nil
}

// holdTmpDir marks an Apply as running, and returns a function that marks
// it finished. The directory is removed when the last running Apply
// finishes, so one finishing does not remove files another is still using.
func holdTmpDir() func() {
	tmp.Lock()
	tmp.applies++
	tmp.Unlock()

	return func() {
		tmp.Lock()
		defer tmp.Unlock()

		tmp.applies--
		if tmp.applies == 0 {
			_ = removeTmpDirLocked()
		}
	}
}

// removeTmpDir removes the run's temporary directory, if one was created.
func removeTmpDir() error {
	tmp.Lock()
	defer tmp.Unlock()

	return removeTmpDirLocked()
}

func removeTmpDirLocked() error {
	if tmp.dir == "" {
		return nil
	}

	err := os.RemoveAll(tmp.dir)
	tmp.dir = ""

	return err
}
