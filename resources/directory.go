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

	// PermissionsOnly manages the mode and ownership of a directory that
	// already exists, applying them to the directory itself and leaving
	// whatever is inside it alone.
	//
	// Only what is set is applied: nothing is defaulted, so a Mode on its own
	// leaves ownership as it is, and a User on its own leaves the mode and the
	// group as they are.
	PermissionsOnly bool

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

	if d.PermissionsOnly {
		if d.Delete {
			return fmt.Errorf("cannot set both Delete and PermissionsOnly")
		}

		// Nothing is defaulted, as for File
		if d.Mode == 0 && !d.managesOwnership() {
			return fmt.Errorf("PermissionsOnly needs one of Mode, User, Group, UID or GID")
		}

		return nil
	}

	return d.preflightPermissions(pdir)
}

func (d *Directory) OperationName() string {
	switch {
	case d.Delete:
		return "Delete"
	case d.PermissionsOnly:
		return "Update"
	default:
		return "Create"
	}
}

func (d *Directory) Run(ctx context.Context, log *viaduct.Logger) error {
	switch {
	case d.Delete:
		return d.deleteDirectory(ctx, log)
	case d.PermissionsOnly:
		return d.setPermissions(ctx, log)
	default:
		return d.createDirectory(ctx, log)
	}
}

// setPermissions applies the mode and ownership to a directory that already
// exists
func (d *Directory) setPermissions(ctx context.Context, log *viaduct.Logger) error {
	path := viaduct.EnvFrom(ctx).Path(viaduct.ExpandPath(d.Path))

	// In a dry run, an earlier resource may be what creates it
	if !viaduct.DirExists(path) {
		if viaduct.EnvFrom(ctx).DryRun {
			log.Info("permissions-managed", "path", path)
			return nil
		}

		return fmt.Errorf("directory does not exist: %s", path)
	}

	var mode os.FileMode
	if d.Mode != 0 {
		mode = os.ModeDir | d.Mode
	}

	return d.applyOnly(ctx, log, path, mode)
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
