package resources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/surminus/viaduct"
)

// Link creates a symlink. If the file exists and is not a symlink, it will
// be created and replaced with the link. If the file exists, is a symlink
// but does not have the right source, it will be replaced.
type Link struct {
	// Path is the path of the symlinked file/directory
	Path string
	// Source is the original file/directory we are linking to
	Source string
	// Delete will delete the symlink.
	Delete bool
	// CreateDirIfMissing creates the parent directory of the symlink if it
	// does not already exist. The parent is created with 0755 and default
	// ownership.
	CreateDirIfMissing bool
}

// CreateLink will create a new symlink.
func CreateLink(path, source string) *Link {
	return &Link{Path: path, Source: source}
}

// DeleteLink will delete a symlink if it exists.
func DeleteLink(path string) *Link {
	return &Link{Path: path, Delete: true}
}

func (l *Link) Description() string {
	return fmt.Sprintf("%s -> %s", l.Source, l.Path)
}

func (l *Link) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (l *Link) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if l.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	// Set optional defaults here
	return nil
}

func (l *Link) OperationName() string {
	if l.Delete {
		return "Delete"
	}

	return "Create"
}

func (l *Link) Run(ctx context.Context, log *viaduct.Logger) error {
	if l.Delete {
		return l.deleteLink(ctx, log)
	}

	return l.createLink(ctx, log)
}

// createLink creates a symlink from Source to Path
func (l *Link) createLink(ctx context.Context, log *viaduct.Logger) error {
	// If creating a link, a source is required, but not if we're deleting.
	if l.Source == "" {
		return fmt.Errorf("required parameter: Source")
	}

	env := viaduct.EnvFrom(ctx)

	// The source should always be the full path. It is the link's target, so
	// the root is not applied to it.
	source, err := filepath.Abs(viaduct.ExpandPath(l.Source))
	if err != nil {
		return err
	}

	path := env.Path(viaduct.ExpandPath(l.Path))

	if l.CreateDirIfMissing {
		if err := ensureParentDir(ctx, log, path); err != nil {
			return err
		}
	}

	// Anything at the path that is not a link to the source is replaced,
	// including a non-link, which makes Readlink fail
	exists := viaduct.LinkExists(path)
	if exists {
		if src, err := os.Readlink(path); err == nil && src == source {
			log.Noop("up-to-date", "source", source, "path", path)
			return nil
		}
	}

	if !change(ctx, log, "created", "source", source, "path", path) {
		return nil
	}

	if exists {
		if err := os.Remove(path); err != nil {
			return err
		}
	}

	return os.Symlink(source, path)
}

// deleteLink deletes the symlink from the Path
func (l *Link) deleteLink(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path := env.Path(viaduct.ExpandPath(l.Path))

	if !viaduct.LinkExists(path) {
		log.Noop("up-to-date", "path", path)
		return nil
	}

	if !change(ctx, log, "deleted", "path", path) {
		return nil
	}

	return os.Remove(path)
}
