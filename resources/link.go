package resources

import (
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
}

// CreateLink will create a new symlink.
func CreateLink(path, source string) *Link {
	return &Link{Path: path, Source: source}
}

// CreateLink will delete a symlink if it exists.
func DeleteLink(path, source string) *Link {
	return &Link{Path: path, Delete: true}
}

func (l *Link) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (l *Link) PreflightChecks(log *viaduct.Logger) error {
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

func (l *Link) Run(log *viaduct.Logger) error {
	if l.Delete {
		return l.deleteLink(log)
	} else {
		return l.createLink(log)
	}
}

// Create can be used in scripting mode to create a symlink from Source to Path
func (l *Link) createLink(log *viaduct.Logger) error {
	// If creating a link, a source is required, but not if we're deleting.
	if l.Source == "" {
		return fmt.Errorf("required parameter: Source")
	}

	// The source should always be the full path, so we will
	// attempt to expand it
	source, err := filepath.Abs(viaduct.ExpandPath(l.Source))
	if err != nil {
		return err
	}

	path := viaduct.ExpandPath(l.Path)
	logmsg := fmt.Sprintf("%s -> %s", source, path)

	if viaduct.Cli.DryRun {
		log.Info(logmsg)
		return nil
	}

	// If the file exists and is a symlink, let's check the source is correct
	if viaduct.LinkExists(path) {
		src, err := os.Readlink(path)
		if err == nil {
			// If the source is not correct, let's delete the symlink
			if src != source {
				if err := os.Remove(path); err != nil {
					return err
				}
			} else {
				// Otherwise everything is as we want it, so return
				log.Noop(logmsg)
				return nil
			}
		} else {
			// If there is an error, then it isn't a symlink. In this case,
			// let's delete the file and replace it with a link.
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}

	if err := os.Symlink(source, path); err != nil {
		return err
	}

	log.Info(logmsg)

	return nil
}

// Delete deletes the symlink from the Path
func (l *Link) deleteLink(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(l.Path)

	if viaduct.Cli.DryRun {
		log.Info(path)
		return nil
	}

	if !viaduct.LinkExists(path) {
		log.Noop(path)
		return nil
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	log.Info(path)

	return nil
}
