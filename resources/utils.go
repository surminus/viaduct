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
// resource (or use Chain) instead. path is expected to be already expanded
// and under the root.
func ensureParentDir(ctx context.Context, log *viaduct.Logger, path string) error {
	dir := filepath.Dir(path)

	if viaduct.DirExists(dir) {
		return nil
	}

	if !change(ctx, log, "created-parent", "path", dir) {
		return nil
	}

	return os.MkdirAll(dir, 0o755)
}

// change logs a change with Info and reports whether to make it, which it
// is not in a dry run.
func change(ctx context.Context, log *viaduct.Logger, msg string, kv ...any) bool {
	log.Info(msg, kv...)

	return !viaduct.EnvFrom(ctx).DryRun
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
	env := viaduct.EnvFrom(ctx)

	// Resolved once, so the content and the permissions go to the same file
	path, err := env.ResolveSymlink(viaduct.ExpandPath(path))
	if err != nil {
		return err
	}

	if createDirIfMissing {
		if err := ensureParentDir(ctx, log, path); err != nil {
			return err
		}
	}

	exists := viaduct.FileExists(path)

	shouldWriteFile := true
	if exists {
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		shouldWriteFile = string(existing) != content
	}

	if !shouldWriteFile {
		log.Noop("up-to-date", "path", path)
		return perms.setFilePermissions(ctx, log, path)
	}

	if !change(ctx, log, "created", "path", path) {
		if !exists {
			// Nothing to set permissions on yet
			return nil
		}

		return perms.setFilePermissions(ctx, log, path)
	}

	if err := viaduct.WriteFileAtomic(path, []byte(content), perms.Mode); err != nil {
		return err
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
	info, err := os.Lstat(path)
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

// openNoFollow opens path to change its mode or ownership through the
// descriptor, so a symlink swapped in at path is never followed. A file the
// user cannot read is opened for writing instead.
func openNoFollow(path string) (*os.File, fs.FileInfo, error) {
	flags := syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC

	f, err := os.OpenFile(path, os.O_RDONLY|flags, 0)
	if errors.Is(err, fs.ErrPermission) {
		f, err = os.OpenFile(path, os.O_WRONLY|flags, 0)
	}

	if errors.Is(err, syscall.ELOOP) {
		return nil, nil, fmt.Errorf("%s is a symlink, and permissions are never set through one", path)
	}

	if err != nil {
		return nil, nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}

	return f, info, nil
}

func applyChmod(ctx context.Context, log *viaduct.Logger, path string, mode os.FileMode) error {
	f, info, err := openNoFollow(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if info.Mode() == mode {
		log.Noop("chmod-unchanged", "path", path, "mode", mode.String())
		return nil
	}

	if !change(ctx, log, "chmod", "path", path, "mode", mode.String()) {
		return nil
	}

	return f.Chmod(mode)
}

func applyChown(ctx context.Context, log *viaduct.Logger, path string, uid, gid int) error {
	f, info, err := openNoFollow(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if ownedBy(info, uid, gid) {
		log.Noop("chown-unchanged", "path", path, "uid", strconv.Itoa(uid), "gid", strconv.Itoa(gid))
		return nil
	}

	if !change(ctx, log, "chown", "path", path, "uid", strconv.Itoa(uid), "gid", strconv.Itoa(gid)) {
		return nil
	}

	return f.Chown(uid, gid)
}

// ownedBy reports whether a file is owned by the user and group.
func ownedBy(info fs.FileInfo, uid, gid int) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)

	return ok && int(stat.Uid) == uid && int(stat.Gid) == gid
}

// lstat and lchown are variables so tests can make a file vanish part way
// through a walk.
var (
	lstat  = os.Lstat
	lchown = os.Lchown
)

// chownTree sets the ownership of everything under root, including root,
// reporting whether anything needed changing. Symlinks are changed
// themselves and never followed, and a file that disappears part way
// through is skipped.
func chownTree(root string, uid, gid int, dryRun bool) (bool, error) {
	var changed bool

	err := filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p != root {
				return nil
			}

			return err
		}

		info, err := lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		if err != nil {
			return err
		}

		if ownedBy(info, uid, gid) {
			return nil
		}

		changed = true

		if dryRun {
			return nil
		}

		if err := lchown(p, uid, gid); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		return nil
	})

	return changed, err
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

	if !recursiveChown || !viaduct.DirExists(path) {
		return applyChown(ctx, log, path, uid, gid)
	}

	changed, err := chownTree(path, uid, gid, viaduct.EnvFrom(ctx).DryRun)
	if err != nil {
		return err
	}

	if changed {
		log.Info("chown-recursive", "path", path, "uid", strconv.Itoa(uid), "gid", strconv.Itoa(gid))
	} else {
		log.Noop("chown-recursive-unchanged", "path", path, "uid", strconv.Itoa(uid), "gid", strconv.Itoa(gid))
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
