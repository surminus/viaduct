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
}

// D is a shortcut for declaring a new Directory resource
func D(path string) Directory {
	return Directory{Path: path}
}

// satisfy sets default values for the parameters for a particular
// resource
func (d *Directory) satisfy(log *logger) {
	// Set required values here, and error if they are not set
	if d.Path == "" {
		log.Fatal("Required parameter: Path")
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
			log.Fatal(err)
		} else {
			d.UID = uid
		}
	}

	if d.Group == "" && d.GID == 0 && !d.Root {
		if gid, err := strconv.Atoi(Attribute.User.Gid); err != nil {
			log.Fatal(err)
		} else {
			d.GID = gid
		}
	}
}

// Create can be used in scripting mode to create or update a directory
func (d Directory) Create() *Directory {
	log := newLogger("Directory", "create")
	err := d.createDirectory(log)
	if err != nil {
		log.Fatal(err)
	}

	return &d
}

// Delete can be used in scripting mode to delete a directory
func (d Directory) Delete() *Directory {
	log := newLogger("Directory", "delete")
	err := d.deleteDirectory(log)
	if err != nil {
		log.Fatal(err)
	}

	return &d
}

// Create creates a directory
func (d Directory) createDirectory(log *logger) error {
	d.satisfy(log)

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

	uid := d.UID
	gid := d.GID

	if d.User != "" {
		u, err := user.Lookup(d.User)
		if err != nil {
			return err
		}

		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return err
		}
	}

	if d.Group != "" {
		g, err := user.LookupGroup(d.Group)
		if err != nil {
			return err
		}

		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			return err
		}
	}

	permlog := newLogger("Directory", "permissions")
	chmodmsg := fmt.Sprintf("%s -> %s", path, d.Mode)
	chownmsg := fmt.Sprintf("%s -> %d:%d", path, uid, gid)

	if MatchChmod(path, d.Mode) {
		permlog.Noop(chmodmsg)
	} else {
		err := os.Chmod(path, d.Mode)
		if err != nil {
			return err
		}

		permlog.Info(chmodmsg)
	}

	if MatchChown(path, uid, gid) {
		permlog.Noop(chownmsg)
	} else {
		err := os.Chown(path, uid, gid)
		if err != nil {
			return err
		}

		permlog.Info(chownmsg)
	}

	return nil
}

// Delete deletes a directory.
func (d Directory) deleteDirectory(log *logger) error {
	d.satisfy(log)

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
