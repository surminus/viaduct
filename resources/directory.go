package resources

import (
	"fmt"
	"os"

	"github.com/surminus/viaduct"
)

// Directory manages a directory on the filesystem
type Directory struct {
	// Path is the path of the directory
	Path string
	// Delete removes the directory if set to true.
	Delete bool

	// NoRecursive applies the ownership to the directory itself, leaving
	// whatever is inside it alone. The default is to apply it to the whole
	// tree.
	NoRecursive bool

	// Permissions manages permissions for the directory
	Permissions
}

// Dir creates a new directory
func Dir(path string) *Directory {
	return &Directory{Path: path}
}

// DirShallow creates a directory whose ownership applies to the directory
// itself, leaving whatever is inside it as it is
func DirShallow(path string) *Directory {
	return &Directory{Path: path, NoRecursive: true}
}

func (d *Directory) Description() string {
	return d.Path
}

func (d *Directory) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (d *Directory) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if d.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	return d.preflightPermissions(pdir)
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

	if viaduct.Cli.DryRun {
		log.Info("created", "path", path)
		return nil
	}

	if !viaduct.DirExists(path) {
		if err := os.MkdirAll(viaduct.ExpandPath(path), d.Mode); err != nil {
			return err
		}

		log.Info("created", "path", path)
	} else {
		log.Noop("up-to-date", "path", path)
	}

	return d.setDirectoryPermissions(
		log,
		path,
		!d.NoRecursive,
	)
}

// Delete deletes a directory.
func (d *Directory) deleteDirectory(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(d.Path)

	if viaduct.Cli.DryRun {
		log.Info("deleted", "path", path)
		return nil
	}

	if viaduct.DirExists(path) {
		if err := os.RemoveAll(viaduct.ExpandPath(d.Path)); err != nil {
			return err
		}
		log.Info("deleted", "path", path)
	} else {
		log.Noop("up-to-date", "path", path)
	}

	return nil
}
