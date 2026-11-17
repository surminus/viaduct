package resources

import (
	"context"
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
func (d *Directory) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
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

func (d *Directory) Run(ctx context.Context, log *viaduct.Logger) error {
	if d.Delete {
		return d.deleteDirectory(ctx, log)
	}

	return d.createDirectory(ctx, log)
}

// createDirectory creates a directory
func (d *Directory) createDirectory(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path := env.Path(viaduct.ExpandPath(d.Path))

	if viaduct.DirExists(path) {
		log.Noop("up-to-date", "path", path)
	} else {
		log.Info("created", "path", path)

		if env.DryRun {
			// Nothing to set permissions on yet
			return nil
		}

		if err := os.MkdirAll(path, d.Mode); err != nil {
			return err
		}
	}

	return d.setDirectoryPermissions(
		ctx,
		log,
		path,
		!d.NoRecursive,
	)
}

// deleteDirectory deletes a directory
func (d *Directory) deleteDirectory(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path := env.Path(viaduct.ExpandPath(d.Path))

	if !viaduct.DirExists(path) {
		log.Noop("up-to-date", "path", path)
		return nil
	}

	if !change(ctx, log, "deleted", "path", path) {
		return nil
	}

	return os.RemoveAll(path)
}
