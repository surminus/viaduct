package viaduct

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// maxSymlinks is how many links ResolveSymlink follows, as the kernel does.
const maxSymlinks = 40

// keptModeBits are the parts of an existing file's mode that a replacement
// keeps.
const keptModeBits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// WriteFileAtomic replaces a file's contents so that a crash leaves either
// the old contents or the new. It is WriteAtomic with the contents in hand.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	return WriteAtomic(path, perm, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// WriteAtomic replaces a file with what write produces, so that a crash
// leaves either the old contents or the new. write is called once, with a
// temporary file in the same directory, which is synced and renamed over the
// original. If write returns an error, the original is left alone.
//
// An existing file keeps its mode and ownership; perm applies only when the
// file is created. A symlink at path is followed as ResolveSymlink describes.
// A file that cannot be replaced, such as a bind mount or one in an
// unwritable directory, is written in place.
func WriteAtomic(path string, perm fs.FileMode, write func(io.Writer) error) error {
	target, err := ResolveSymlink(path)
	if err != nil {
		return err
	}

	mode := perm
	uid, gid := -1, -1

	info, err := os.Stat(target)
	switch {
	case err == nil:
		mode = info.Mode() & keptModeBits

		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(stat.Uid), int(stat.Gid)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}

	dir := filepath.Dir(target)

	// No suffix, so a leftover file is not picked up by globs such as *.conf
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".viaduct-*")
	if err != nil {
		// The file may be writable in a directory that is not
		if uid >= 0 && errors.Is(err, fs.ErrPermission) {
			return writeInPlace(target, write)
		}

		return err
	}

	var closed, renamed bool

	defer func() {
		if !closed {
			tmp.Close()
		}

		if !renamed {
			os.Remove(tmp.Name())
		}
	}()

	if err := write(tmp); err != nil {
		return err
	}

	if err := tmp.Sync(); err != nil {
		return err
	}

	// Set on the open file, so the name cannot be swapped in between. The
	// owner goes first, since changing it clears setuid and setgid.
	if uid >= 0 {
		if err := tmp.Chown(uid, gid); err != nil {
			// Without privileges the owner cannot be preserved on a new file
			if errors.Is(err, fs.ErrPermission) {
				return copyInPlace(tmp, target)
			}

			return err
		}
	}

	if err := tmp.Chmod(mode); err != nil {
		return err
	}

	closed = true
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		// A bind-mounted file cannot be replaced
		if errors.Is(err, syscall.EBUSY) || errors.Is(err, syscall.EXDEV) {
			return copyFileInPlace(tmp.Name(), target)
		}

		return err
	}

	renamed = true

	return syncDir(dir)
}

// ResolveSymlink returns path with every symlink in it resolved. A symlink,
// at path or in any directory above it, is followed only when the effective
// user or root owns it, including a link whose target does not exist yet.
// Any other symlink is an error. Whatever does not exist yet is joined on as
// it is.
func ResolveSymlink(path string) (string, error) {
	return resolveIn(string(filepath.Separator), path)
}

// resolveIn resolves path as though root were "/": an absolute link starts
// again from root, and ".." stops at it. Each link is resolved one hop at a
// time, so a link reached through another link's target is checked too.
func resolveIn(root, path string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}

	abs := path
	if !filepath.IsAbs(abs) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}

		abs = wd + string(filepath.Separator) + path
	}

	rel, err := filepath.Rel(root, filepath.Clean(abs))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not under %s", path, root)
	}

	// The path is walked as written, not cleaned, so a ".." after a symlink
	// goes up from where the link leads, as the kernel does. A path that
	// does not start with root as written, such as one with "." in root's
	// part of it, is walked cleaned.
	prefix := root
	if root != string(filepath.Separator) {
		prefix += string(filepath.Separator)
	}

	below, ok := strings.CutPrefix(abs, prefix)
	if !ok {
		below = rel
	}

	resolved := root
	rest := strings.Split(below, string(filepath.Separator))
	followed := 0
	missing := false

	for len(rest) > 0 {
		part := rest[0]
		rest = rest[1:]

		switch part {
		case "", ".":
			continue
		case "..":
			if resolved != root {
				resolved = filepath.Dir(resolved)
			}

			// What is above a missing path may exist, and may be a link
			missing = false

			continue
		}

		next := filepath.Join(resolved, part)

		// Below a path that does not exist there is nothing to follow
		if !missing {
			info, err := os.Lstat(next)

			switch {
			case errors.Is(err, fs.ErrNotExist):
				missing = true
			case err != nil:
				return "", err
			case info.Mode()&fs.ModeSymlink != 0:
				if followed++; followed > maxSymlinks {
					return "", fmt.Errorf("%s: %w", path, syscall.ELOOP)
				}

				if err := checkSymlinkOwner(next, info); err != nil {
					return "", err
				}

				link, err := os.Readlink(next)
				if err != nil {
					return "", err
				}

				// A relative link is resolved from the directory holding it,
				// which is resolved already
				if filepath.IsAbs(link) {
					resolved = root
				}

				rest = append(strings.Split(link, string(filepath.Separator)), rest...)

				continue
			}
		}

		resolved = next
	}

	return resolved, nil
}

// checkSymlinkOwner refuses a link owned by anyone but the effective user or
// root. Running as root, that stops a user who controls a directory, such as
// their home, from pointing a managed file at one of root's.
func checkSymlinkOwner(path string, info fs.FileInfo) error {
	link, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot read the owner of %s", path)
	}

	if !mayFollowSymlink(int(link.Uid), os.Geteuid()) {
		return fmt.Errorf("will not follow symlink %s: owned by uid %d, not the effective user or root", path, link.Uid)
	}

	return nil
}

// mayFollowSymlink reports whether a link may be followed, given its owner
// and the effective user. Only root can create a link owned by root, so
// following one gives no other user a way in.
func mayFollowSymlink(linkUID, euid int) bool {
	return linkUID == euid || linkUID == 0
}

// writeInPlace truncates and rewrites a file, for when it cannot be
// replaced. The content is produced before the file is truncated, so an
// error from write leaves the file as it was.
func writeInPlace(path string, write func(io.Writer) error) error {
	var buf bytes.Buffer

	if err := write(&buf); err != nil {
		return err
	}

	return copyInPlace(&buf, path)
}

// copyFileInPlace copies a finished temporary file over path in place.
func copyFileInPlace(src, path string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	return copyInPlace(f, path)
}

// copyInPlace truncates path and copies src into it. An *os.File is read
// from the start.
func copyInPlace(src io.Reader, path string) error {
	if f, ok := src.(*os.File); ok {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		return err
	}

	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

// syncDir makes a rename in the directory durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()

	if err := d.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return fmt.Errorf("syncing %s: %w", dir, err)
	}

	return nil
}
