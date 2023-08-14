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

	// Permissions manages permissions for the directory
	Permissions
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

	return d.setDirectoryPermissions(
		log,
		path,
		true,
	)
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
