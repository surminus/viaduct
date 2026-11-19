package resources

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/surminus/viaduct"
)

// CopyDir copies a directory tree, writing only the entries that differ.
// Symlinks are copied as symlinks, and anything other than a file, directory
// or symlink fails the copy. Each entry takes its mode from Source and is
// owned by the user running viaduct. Entries in Path that are not in Source
// are left alone.
//
// The copy is logged as one line, with how many entries were created,
// updated or had their mode changed.
type CopyDir struct {
	// Source is the directory to copy, read when the resource runs, so it
	// can be one that an earlier resource creates. Required.
	Source string

	// Path is the directory to copy Source to, created with the mode of
	// Source if it does not exist. Required.
	Path string
}

func (c *CopyDir) Description() string {
	return c.Path
}

func (c *CopyDir) OperationName() string {
	return "Copy"
}

func (c *CopyDir) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

func (c *CopyDir) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	if c.Source == "" {
		return fmt.Errorf("required parameter: Source")
	}

	if c.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	source := filepath.Clean(viaduct.ExpandPath(c.Source))
	path := filepath.Clean(viaduct.ExpandPath(c.Path))

	if inDir(path, source) || inDir(source, path) {
		return fmt.Errorf("cannot copy %s to %s, since one contains the other", source, path)
	}

	return nil
}

// inDir reports whether path is dir or somewhere beneath it
func inDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)

	return err == nil && filepath.IsLocal(rel)
}

func (c *CopyDir) Run(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	source := env.Path(viaduct.ExpandPath(c.Source))
	path := env.Path(viaduct.ExpandPath(c.Path))

	info, err := os.Stat(source)
	switch {
	case errors.Is(err, fs.ErrNotExist) && env.DryRun:
		// An earlier resource would have created it
		log.Info("copied", "path", path, "source", source)
		return nil
	case err != nil:
		return fmt.Errorf("reading Source: %w", err)
	case !info.IsDir():
		return fmt.Errorf("source is not a directory: %s", source)
	}

	copier := dirCopier{dryRun: env.DryRun, counts: map[string]int{}, absent: map[string]bool{}}

	dest, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && env.DryRun:
		// Everything in Source would be created, but there is no Path to
		// compare it with
		log.Info("copied", "path", path, "source", source)
		return nil
	case errors.Is(err, fs.ErrNotExist):
		copier.counts["created"]++

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}

		// Path is given the mode of Source once everything is in it, in
		// case that mode stops the user writing to it
		if err := os.Mkdir(path, 0o700); err != nil {
			return err
		}

		copier.later(".", copyMode(info))
	case err != nil:
		return err
	case !dest.IsDir():
		return fmt.Errorf("path is not a directory: %s", path)
	}

	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()

	copier.root = root

	err = filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(source, name)
		if err != nil || rel == "." {
			return err
		}

		if err := copier.copy(name, rel, entry); err != nil {
			return fmt.Errorf("copying %s: %w", rel, err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	if err := copier.finish(); err != nil {
		return err
	}

	if len(copier.counts) == 0 {
		log.Noop("up-to-date", "path", path, "source", source)
		return nil
	}

	// One line for the whole tree, however many entries changed
	fields := []any{"path", path, "source", source}
	for _, msg := range []string{"created", "updated", "chmod"} {
		if n := copier.counts[msg]; n > 0 {
			fields = append(fields, msg, strconv.Itoa(n))
		}
	}

	log.Info("copied", fields...)

	return nil
}

// copyMode is the mode an entry is given in Path: its permissions, and the
// setuid, setgid and sticky bits
func copyMode(info fs.FileInfo) fs.FileMode {
	return info.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
}

// dirCopier copies the entries of Source into Path. Every change goes through
// root, so a symlink already in Path is never followed out of it.
type dirCopier struct {
	root   *os.Root
	dryRun bool

	// counts is how many entries each kind of change, such as "created",
	// applies to, logged together once the copy has finished
	counts map[string]int

	// absent holds the directories a dry run would have created or
	// replaced. What is at their path now says nothing about their entries,
	// which are all counted as created.
	absent map[string]bool

	// modes are the directory modes to set once their entries are written,
	// in the order the directories were reached
	modes []dirMode
}

type dirMode struct {
	rel  string
	mode fs.FileMode
}

// later sets the mode of the directory at rel once the copy has finished
func (c *dirCopier) later(rel string, mode fs.FileMode) {
	c.modes = append(c.modes, dirMode{rel, mode})
}

// finish sets the directory modes, deepest first, since a directory that is
// not writable any more cannot have its entries changed
func (c *dirCopier) finish() error {
	for i := len(c.modes) - 1; i >= 0; i-- {
		if err := c.root.Chmod(c.modes[i].rel, c.modes[i].mode); err != nil {
			return err
		}
	}

	return nil
}

// change counts a change, returning whether to make it, which it is not in a
// dry run
func (c *dirCopier) change(msg string) bool {
	c.counts[msg]++

	return !c.dryRun
}

func (c *dirCopier) copy(name, rel string, entry fs.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}

	var existing fs.FileInfo
	if !c.absent[filepath.Dir(rel)] {
		existing, err = c.root.Lstat(rel)
		if errors.Is(err, fs.ErrNotExist) {
			existing, err = nil, nil
		}
		if err != nil {
			return err
		}
	}

	switch {
	case info.IsDir():
		return c.copyDir(rel, info, existing)
	case info.Mode().IsRegular():
		return c.copyFile(name, rel, info, existing)
	case info.Mode()&fs.ModeSymlink != 0:
		return c.copySymlink(name, rel, existing)
	default:
		return fmt.Errorf("not a file, directory or symlink")
	}
}

func (c *dirCopier) copyDir(rel string, info, existing fs.FileInfo) error {
	mode := copyMode(info)

	if existing != nil && existing.IsDir() {
		if copyMode(existing) == mode {
			return nil
		}

		if c.change("chmod") {
			c.later(rel, mode)
		}

		return nil
	}

	if !c.change("created") {
		c.absent[rel] = true
		return nil
	}

	if existing != nil {
		if err := c.root.Remove(rel); err != nil {
			return err
		}
	}

	// Writable until its entries are in it, whatever its mode in Source
	if err := c.root.Mkdir(rel, 0o700); err != nil {
		return err
	}

	c.later(rel, mode)

	return nil
}

func (c *dirCopier) copyFile(name, rel string, info, existing fs.FileInfo) error {
	mode := copyMode(info)

	if existing != nil && existing.IsDir() {
		return fmt.Errorf("a directory is in the way")
	}

	if existing != nil && existing.Mode().IsRegular() {
		same, err := c.sameContent(name, rel, info, existing)
		if err != nil {
			return err
		}

		if same {
			if copyMode(existing) == mode {
				return nil
			}

			if !c.change("chmod") {
				return nil
			}

			return c.root.Chmod(rel, mode)
		}
	}

	msg := "created"
	if existing != nil {
		msg = "updated"
	}

	if !c.change(msg) {
		return nil
	}

	src, err := os.Open(name)
	if err != nil {
		return err
	}
	defer src.Close()

	// Written beside the file and renamed over it, so the file is never seen
	// half written. The random suffix keeps it from replacing an entry from
	// Source that happens to have the same name.
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}

	tmp := filepath.Join(filepath.Dir(rel), fmt.Sprintf(".%s.viaduct-%x", filepath.Base(rel), suffix))
	if err := writeEntry(c.root, tmp, src, mode, nil); err != nil {
		_ = c.root.Remove(tmp)
		return err
	}

	if err := c.root.Rename(tmp, rel); err != nil {
		_ = c.root.Remove(tmp)
		return err
	}

	return nil
}

func (c *dirCopier) copySymlink(name, rel string, existing fs.FileInfo) error {
	target, err := os.Readlink(name)
	if err != nil {
		return err
	}

	if existing != nil && existing.Mode()&fs.ModeSymlink != 0 {
		current, err := c.root.Readlink(rel)
		if err != nil {
			return err
		}

		if current == target {
			return nil
		}
	}

	msg := "created"
	if existing != nil {
		msg = "updated"
	}

	if !c.change(msg) {
		return nil
	}

	if err := replace(c.root, rel, false); err != nil {
		return err
	}

	return c.root.Symlink(target, rel)
}

// sameContent compares a file in Source with the one at rel in Path, a block
// at a time, so neither is read into memory whole
func (c *dirCopier) sameContent(name, rel string, info, existing fs.FileInfo) (bool, error) {
	if info.Size() != existing.Size() {
		return false, nil
	}

	src, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer src.Close()

	dst, err := c.root.Open(rel)
	if err != nil {
		return false, err
	}
	defer dst.Close()

	bufA := make([]byte, 32*1024)
	bufB := make([]byte, 32*1024)

	for {
		n, errA := io.ReadFull(src, bufA)
		m, errB := io.ReadFull(dst, bufB)

		if !bytes.Equal(bufA[:n], bufB[:m]) {
			return false, nil
		}

		doneA := errors.Is(errA, io.EOF) || errors.Is(errA, io.ErrUnexpectedEOF)
		doneB := errors.Is(errB, io.EOF) || errors.Is(errB, io.ErrUnexpectedEOF)

		switch {
		case errA != nil && !doneA:
			return false, errA
		case errB != nil && !doneB:
			return false, errB
		case doneA || doneB:
			return doneA && doneB, nil
		}
	}
}
