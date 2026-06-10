package resources

import (
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"

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

// runCommand runs a system command, directing output according to the
// CLI flags
func runCommand(args ...string) error {
	// nolint:gosec
	cmd := exec.Command(args[0], args[1:]...)
	setCommandOutput(cmd)
	return cmd.Run()
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
		if uid, err := strconv.Atoi(viaduct.Attribute.User.Uid); err != nil {
			return err
		} else {
			p.UID = uid
		}
	}

	if p.Group == "" && p.GID == 0 && !p.Root {
		if gid, err := strconv.Atoi(viaduct.Attribute.User.Gid); err != nil {
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

func applyChmod(log *viaduct.Logger, path string, mode os.FileMode) error {
	if viaduct.MatchChmod(path, mode) {
		log.Noop("chmod-unchanged", "path", path, "mode", mode.String())
		return nil
	}

	if err := os.Chmod(path, mode); err != nil {
		return err
	}

	log.Info("chmod", "path", path, "mode", mode.String())
	return nil
}

func applyChown(log *viaduct.Logger, path string, uid, gid int) error {
	if viaduct.MatchChown(path, uid, gid) {
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
	log *viaduct.Logger,
	path string,
	recursiveChown bool,
) error {
	uid, gid, err := p.resolveOwnership()
	if err != nil {
		return err
	}

	if err := applyChmod(log, path, p.Mode); err != nil {
		return err
	}

	if viaduct.IsDirectory(path) && recursiveChown {
		var wasUpdated bool

		files, err := viaduct.ListFiles(path)
		if err != nil {
			return err
		}

		for _, f := range files {
			if viaduct.MatchChown(f, uid, gid) {
				continue
			}

			wasUpdated = true
			if err := os.Chown(f, uid, gid); err != nil {
				return err
			}
		}

		if wasUpdated {
			log.Info("chown-recursive", "path", path, "uid", strconv.Itoa(uid), "gid", strconv.Itoa(gid))
		} else {
			log.Noop("chown-recursive-unchanged", "path", path, "uid", strconv.Itoa(uid), "gid", strconv.Itoa(gid))
		}
	} else {
		if err := applyChown(log, path, uid, gid); err != nil {
			return err
		}
	}

	return nil
}

// Set permissions for a file
func (p *Permissions) setFilePermissions(
	log *viaduct.Logger,
	path string,
) error {
	uid, gid, err := p.resolveOwnership()
	if err != nil {
		return err
	}

	if err := applyChown(log, path, uid, gid); err != nil {
		return err
	}

	return applyChmod(log, path, p.Mode)
}
