package resources

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"strconv"

	"github.com/surminus/viaduct"
)

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

// Set permissions for a directory
func (p *Permissions) setDirectoryPermissions(
	log *viaduct.Logger,
	path string,
	recursiveChown bool,
) error {
	uid := p.UID
	gid := p.GID
	mode := p.Mode

	if p.User != "" {
		u, err := user.Lookup(p.User)
		if err != nil {
			return err
		}

		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return err
		}
	}

	if p.Group != "" {
		g, err := user.LookupGroup(p.Group)
		if err != nil {
			return err
		}

		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			return err
		}
	}

	chmodmsg := fmt.Sprintf("Permissions: %s -> %s", path, mode)
	chownmsg := fmt.Sprintf("Permissions: %s -> %d:%d", path, uid, gid)

	if viaduct.MatchChmod(path, mode) {
		log.Noop(chmodmsg)
	} else {
		err := os.Chmod(path, mode)
		if err != nil {
			return err
		}

		log.Info(chmodmsg)
	}

	// If it's a directory, recursively set ownership permissions
	if viaduct.IsDirectory(path) && recursiveChown {
		chownmsg += " (recursive)"
		var wasupdated bool

		if files, err := viaduct.ListFiles(path); err == nil {
			for _, f := range files {
				if viaduct.MatchChown(f, uid, gid) {
					continue
				}

				wasupdated = true
				if err := os.Chown(f, uid, gid); err != nil {
					return err
				}
			}
		} else {
			return err
		}

		if wasupdated {
			log.Info(chownmsg)
		} else {
			log.Noop(chownmsg)
		}
	} else {
		if viaduct.MatchChown(path, uid, gid) {
			log.Noop(chownmsg)
		} else {
			if err := os.Chown(path, uid, gid); err != nil {
				return err
			}

			log.Info(chownmsg)
		}
	}

	return nil
}

// Set permissions for a file
func (p *Permissions) setFilePermissions(
	log *viaduct.Logger,
	path string,
) error {
	uid := p.UID
	gid := p.GID
	mode := p.Mode

	if p.User != "" {
		u, err := user.Lookup(p.User)
		if err != nil {
			return err
		}

		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return err
		}
	}

	if p.Group != "" {
		g, err := user.LookupGroup(p.Group)
		if err != nil {
			return err
		}

		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			return err
		}
	}

	chmodmsg := fmt.Sprintf("Permissions: %s -> %s", path, mode)
	chownmsg := fmt.Sprintf("Permissions: %s -> %d:%d", path, uid, gid)

	if viaduct.MatchChown(path, uid, gid) {
		log.Noop(chownmsg)
	} else {
		if err := os.Chown(path, uid, gid); err != nil {
			return err
		}
		log.Info(chownmsg)
	}

	if viaduct.MatchChmod(path, mode) {
		log.Noop(chmodmsg)
	} else {
		if err := os.Chown(path, uid, gid); err != nil {
			return err
		}
		log.Info(chownmsg)
	}

	return nil
}
