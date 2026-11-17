package resources

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/surminus/viaduct"
)

// pathMutexes serialises resources that do read-modify-write on a shared
// file, keyed by path. Resources run concurrently and the global lock is
// too coarse, so this gives same-file edits their own lock while leaving
// edits to different files parallel.
var pathMutexes sync.Map

// lockPath acquires the lock for a file path and returns the unlock
// function. The path is cleaned so equivalent paths share a lock.
func lockPath(path string) func() {
	mu, _ := pathMutexes.LoadOrStore(filepath.Clean(path), &sync.Mutex{})
	m := mu.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

// ensureParentDir creates the parent directory of the given path if it does
// not already exist. It backs the CreateDirIfMissing option on the
// path-writing resources, letting a resource make its own parent without a
// separately declared Directory resource.
//
// The parent is created with 0755 and default ownership. If you need the
// parent managed with specific permissions or ownership, declare a Directory
// resource (or use Chain) instead. path is expected to be already expanded.
func ensureParentDir(ctx context.Context, log *viaduct.Logger, path string) error {
	dir := filepath.Dir(path)

	if viaduct.DirExists(dir) {
		return nil
	}

	if viaduct.EnvFrom(ctx).DryRun {
		log.Info("created-parent", "path", dir)
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	log.Info("created-parent", "path", dir)
	return nil
}

// writeManagedFile writes content to path when it differs from what is
// already there, then applies permissions. File and Template share it so a
// rendered template goes through the same create-or-update lifecycle as a
// managed file, without either resource reaching into the other.
//
// The caller is responsible for having run preflight checks, since the mode
// and ownership in perms are resolved there.
func writeManagedFile(
	ctx context.Context,
	log *viaduct.Logger,
	path, content string,
	perms *Permissions,
	createDirIfMissing bool,
) error {
	path = viaduct.ExpandPath(path)

	if createDirIfMissing {
		if err := ensureParentDir(ctx, log, path); err != nil {
			return err
		}
	}

	if viaduct.EnvFrom(ctx).DryRun {
		log.Info("created", "path", path)
		return nil
	}

	shouldWriteFile := true
	if viaduct.FileExists(path) {
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		shouldWriteFile = string(existing) != content
	}

	if shouldWriteFile {
		if err := os.WriteFile(path, []byte(content), perms.Mode); err != nil {
			return err
		}

		log.Info("created", "path", path)
	} else {
		log.Noop("up-to-date", "path", path)
	}

	return perms.setFilePermissions(ctx, log, path)
}

// ownership is the user and group that own a path
type ownership struct {
	uid int
	gid int
}

// fileOwnership returns who owns a path, so a resource managing one side of the
// ownership can leave the other side as it is
func fileOwnership(path string) (ownership, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ownership{}, err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ownership{}, fmt.Errorf("cannot read ownership of %s", path)
	}

	return ownership{uid: int(stat.Uid), gid: int(stat.Gid)}, nil
}

// runCommand runs a command through the environment's runner
func runCommand(ctx context.Context, log *viaduct.Logger, args ...string) error {
	return viaduct.Command{Name: args[0], Args: args[1:]}.Run(ctx, log)
}

// Permissions can be used with some resources to manage how they set
// permissions on files
type Permissions struct {
	// Mode is the permissions set of the file
	Mode os.FileMode
	// User sets the user permissions by user name
	User string
	// Group sets the group permissions by group name
	Group string
	// UID sets the user permissions by UID
	UID int
	// GID sets the group permissions by GID
	GID int
	// Root enforces using the root user
	Root bool
}

type ptype string

const (
	pdir  ptype = "directory"
	pfile ptype = "file"
)

const (
	DefaultDirectoryPermissions fs.FileMode = os.ModeDir | 0o755
	DefaultFilePermissions      fs.FileMode = 0o644
)

func (p *Permissions) preflightPermissions(t ptype) error {
	if p.Mode == 0 {
		if t == pdir {
			p.Mode = DefaultDirectoryPermissions
		}

		if t == pfile {
			p.Mode = DefaultFilePermissions
		}
	} else {
		if t == pdir {
			// Explicity set modedir to avoid diffs
			p.Mode = os.ModeDir | p.Mode
		}
	}

	if p.User == "" && p.UID == 0 && !p.Root {
		if uid, err := strconv.Atoi(viaduct.Attributes().User.Uid); err != nil {
			return err
		} else {
			p.UID = uid
		}
	}

	if p.Group == "" && p.GID == 0 && !p.Root {
		if gid, err := strconv.Atoi(viaduct.Attributes().User.Gid); err != nil {
			return err
		} else {
			p.GID = gid
		}
	}

	return nil
}

// resolveOwnership resolves User/Group names to UID/GID, falling back
// to the numeric UID/GID already set on the Permissions struct.
func (p *Permissions) resolveOwnership() (uid, gid int, err error) {
	uid = p.UID
	gid = p.GID

	if p.User != "" {
		u, err := user.Lookup(p.User)
		if err != nil {
			return 0, 0, err
		}

		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return 0, 0, err
		}
	}

	if p.Group != "" {
		g, err := user.LookupGroup(p.Group)
		if err != nil {
			return 0, 0, err
		}

		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			return 0, 0, err
		}
	}

	return uid, gid, nil
}

func applyChmod(ctx context.Context, log *viaduct.Logger, path string, mode os.FileMode) error {
	match, err := viaduct.MatchChmod(path, mode)
	if err != nil {
		return err
	}

	if match {
		log.Noop("chmod-unchanged", "path", path, "mode", mode.String())
		return nil
	}

	if err := os.Chmod(path, mode); err != nil {
		return err
	}

	log.Info("chmod", "path", path, "mode", mode.String())
	return nil
}

func applyChown(ctx context.Context, log *viaduct.Logger, path string, uid, gid int) error {
	match, err := viaduct.MatchChown(path, uid, gid)
	if err != nil {
		return err
	}

	if match {
		log.Noop("chown-unchanged", "path", path, "uid", strconv.Itoa(uid), "gid", strconv.Itoa(gid))
		return nil
	}

	if err := os.Chown(path, uid, gid); err != nil {
		return err
	}

	log.Info("chown", "path", path, "uid", strconv.Itoa(uid), "gid", strconv.Itoa(gid))
	return nil
}

// Set permissions for a directory
func (p *Permissions) setDirectoryPermissions(
	ctx context.Context,
	log *viaduct.Logger,
	path string,
	recursiveChown bool,
) error {
	uid, gid, err := p.resolveOwnership()
	if err != nil {
		return err
	}

	if err := applyChmod(ctx, log, path, p.Mode); err != nil {
		return err
	}

	if viaduct.DirExists(path) && recursiveChown {
		var wasUpdated bool

		files, err := viaduct.ListFiles(path)
		if err != nil {
			return err
		}

		for _, f := range files {
			match, err := viaduct.MatchChown(f, uid, gid)
			if errors.Is(err, fs.ErrNotExist) {
				// Removed since the walk found it
				continue
			} else if err != nil {
				return err
			}

			if match {
				continue
			}

			wasUpdated = true
			if err := os.Chown(f, uid, gid); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}

		if wasUpdated {
			log.Info("chown-recursive", "path", path, "uid", strconv.Itoa(uid), "gid", strconv.Itoa(gid))
		} else {
			log.Noop("chown-recursive-unchanged", "path", path, "uid", strconv.Itoa(uid), "gid", strconv.Itoa(gid))
		}
	} else {
		if err := applyChown(ctx, log, path, uid, gid); err != nil {
			return err
		}
	}

	return nil
}

// Set permissions for a file
func (p *Permissions) setFilePermissions(
	ctx context.Context,
	log *viaduct.Logger,
	path string,
) error {
	uid, gid, err := p.resolveOwnership()
	if err != nil {
		return err
	}

	if err := applyChown(ctx, log, path, uid, gid); err != nil {
		return err
	}

	return applyChmod(ctx, log, path, p.Mode)
}
