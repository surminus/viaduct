package resources

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"github.com/surminus/viaduct"
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

// Dir creates a new directory
func Dir(path string) *Directory {
	return &Directory{Path: path}
}

func (d *Directory) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (d *Directory) PreflightChecks(log *viaduct.Logger) error {
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
		if uid, err := strconv.Atoi(viaduct.Attribute.User.Uid); err != nil {
			return err
		} else {
			d.UID = uid
		}
	}

	if d.Group == "" && d.GID == 0 && !d.Root {
		if gid, err := strconv.Atoi(viaduct.Attribute.User.Gid); err != nil {
			return err
		} else {
			d.GID = gid
		}
	}

	return nil
}

func (d *Directory) OperationName() string {
	if d.Delete {
		return "Delete"
	}

	return "Create"
}

func (d *Directory) Run(log *viaduct.Logger) error {
	if d.Delete {
		return d.deleteDirectory(log)
	} else {
		return d.createDirectory(log)
	}
}

// Create creates a directory
func (d *Directory) createDirectory(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(d.Path)

	if viaduct.Config.DryRun {
		log.Info(d.Path)
		return nil
	}

	if !viaduct.DirExists(path) {
		if err := os.MkdirAll(viaduct.ExpandPath(path), d.Mode); err != nil {
			return err
		}

		log.Info(d.Path)
	} else {
		log.Noop(d.Path)
	}

	return setDirectoryPermissions(
		log,
		path,
		d.UID, d.GID,
		d.User, d.Group,
		d.Mode,
		true,
	)
}

func setDirectoryPermissions(
	log *viaduct.Logger,
	path string,
	uid, gid int,
	username, group string,
	mode os.FileMode,
	recursiveChown bool,
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

// Delete deletes a directory.
func (d *Directory) deleteDirectory(log *viaduct.Logger) error {
	if viaduct.Config.DryRun {
		log.Info(d.Path)
		return nil
	}

	path := viaduct.ExpandPath(d.Path)

	if viaduct.DirExists(path) {
		if err := os.RemoveAll(viaduct.ExpandPath(d.Path)); err != nil {
			return err
		}
		log.Info(d.Path)
	} else {
		log.Noop(d.Path)
	}

	return nil
}
