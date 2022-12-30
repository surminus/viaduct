package viaduct

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
)

// Directory manages a directory on the filesystem
type Directory struct {
	// Path is the path of the directory
	Path string
	// Mode is the permissions set of the directory
	Mode os.FileMode

	// User sets the user permissions by user name
	User string
	// Group sets the group permissions by group name
	Group string
	// UID sets the user permissions by UID
	UID int
	// GID sets the group permissions by GID
	GID int
	// Root enforces the use of the root user
	Root bool
	// Delete removes the directory if set to true.
	Delete bool
}

// D is a shortcut for declaring a new Directory resource
func D(path string) Directory {
	return Directory{Path: path}
}

// satisfy sets default values for the parameters for a particular
// resource
func (d *Directory) satisfy(log *logger) error {
	// Set required values here, and error if they are not set
	if d.Path == "" {
		return fmt.Errorf("Required parameter: Path")
	}

	// Set optional defaults here
	if d.Mode == 0 {
		d.Mode = os.ModeDir | 0755
	} else {
		// Explicity set modedir to avoid diffs
		d.Mode = os.ModeDir | d.Mode
	}

	if d.User == "" && d.UID == 0 && !d.Root {
		if uid, err := strconv.Atoi(Attribute.User.Uid); err != nil {
			return err
		} else {
			d.UID = uid
		}
	}

	if d.Group == "" && d.GID == 0 && !d.Root {
		if gid, err := strconv.Atoi(Attribute.User.Gid); err != nil {
			return err
		} else {
			d.GID = gid
		}
	}

	return nil
}

func (d Directory) operationName() string {
	if d.Delete {
		return "Delete"
	}

	return "Create"
}

func (d Directory) run() error {
	log := newLogger("Directory", d.operationName())

	if err := d.satisfy(log); err != nil {
		return err
	}

	if d.Delete {
		return d.deleteDirectory(log)
	} else {
		return d.createDirectory(log)
	}
}

// Create creates a directory
func (d Directory) createDirectory(log *logger) error {
	path := ExpandPath(d.Path)

	if Config.DryRun {
		log.Info(d.Path)
		return nil
	}

	if !DirExists(path) {
		if err := os.MkdirAll(ExpandPath(path), d.Mode); err != nil {
			return err
		}

		log.Info(d.Path)
	} else {
		log.Noop(d.Path)
	}

	return setDirectoryPermissions(
		newLogger("Directory", "permissions"),
		path,
		d.UID, d.GID,
		d.User, d.Group,
		d.Mode,
	)
}

func setDirectoryPermissions(
	log *logger,
	path string,
	uid, gid int,
	username, group string,
	mode os.FileMode,
) error {
	if username != "" {
		u, err := user.Lookup(username)
		if err != nil {
			return err
		}

		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return err
		}
	}

	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return err
		}

		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			return err
		}
	}

	chmodmsg := fmt.Sprintf("%s -> %s", path, mode)
	chownmsg := fmt.Sprintf("%s -> %d:%d", path, uid, gid)

	if MatchChmod(path, mode) {
		log.Noop(chmodmsg)
	} else {
		err := os.Chmod(path, mode)
		if err != nil {
			return err
		}

		log.Info(chmodmsg)
	}

	if MatchChown(path, uid, gid) {
		log.Noop(chownmsg)
	} else {
		err := os.Chown(path, uid, gid)
		if err != nil {
			return err
		}

		log.Info(chownmsg)
	}

	return nil
}

// Delete deletes a directory.
func (d Directory) deleteDirectory(log *logger) error {
	if Config.DryRun {
		log.Info(d.Path)
		return nil
	}

	path := ExpandPath(d.Path)

	if DirExists(path) {
		if err := os.RemoveAll(ExpandPath(d.Path)); err != nil {
			return err
		}
		log.Info(d.Path)
	} else {
		log.Noop(d.Path)
	}

	return nil
}
