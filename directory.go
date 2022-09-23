package viaduct

import (
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
		d.Mode = 0o755
	}

	if d.User == "" && d.UID == 0 {
		if uid, err := strconv.Atoi(Attribute.User.Uid); err != nil {
			log.Fatal(err)
		} else {
			d.UID = uid
		}
	}

	if d.Group == "" && d.GID == 0 {
		if gid, err := strconv.Atoi(Attribute.User.Gid); err != nil {
			log.Fatal(err)
		} else {
			d.GID = gid
		}
	}
}

// Create creates a directory
func (d Directory) Create() *Directory {
	log := newLogger("Directory", "create")
	d.satisfy(log)

	log.Info(d.Path)
	if Config.DryRun {
		return &d
	}

	path := ExpandPath(d.Path)

	err := os.MkdirAll(ExpandPath(path), d.Mode)
	if err != nil {
		log.Fatal(err)
	}

	uid := d.UID
	gid := d.GID

	if d.User != "" {
		u, err := user.Lookup(d.User)
		if err != nil {
			log.Fatal(err)
		}

		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			log.Fatal(err)
		}
	}

	if d.Group != "" {
		g, err := user.LookupGroup(d.Group)
		if err != nil {
			log.Fatal(err)
		}

		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			log.Fatal(err)
		}
	}

	err = os.Chown(path, uid, gid)
	if err != nil {
		log.Fatal(err)
	}

	return &d
}

// Delete deletes a directory.
func (d Directory) Delete() *Directory {
	log := newLogger("Directory", "delete")
	d.satisfy(log)

	log.Info(d.Path)

	if Config.DryRun {
		return &d
	}

	if err := os.RemoveAll(ExpandPath(d.Path)); err != nil {
		log.Fatal(err)
	}

	return &d
}
