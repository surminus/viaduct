package viaduct

import (
	"os"
	"path/filepath"
)

type Link struct {
	// Path is the path of the symlinked file/directory
	Path string
	// Source is the original file/directory we are linking to
	Source string
}

// satisfy sets default values for the parameters for a particular
// resource
func (l *Link) satisfy(log *logger) {
	// Set required values here, and error if they are not set
	if l.Path == "" {
		log.Fatal("Required parameter: Path")
	}

	// Set optional defaults here
}

// Create creates a new symlink from Source to Path
func (l Link) Create() *Link {
	log := newLogger("Link", "create")
	l.satisfy(log)

	// We must specify the source if we are performing a link
	if l.Source == "" {
		log.Fatal("Required parameter: Source")
	}

	log.Info(l.Path, " -> ", l.Source)
	if Config.DryRun {
		return &l
	}

	// The source should always be the full path, so we will
	// attempt to expand it
	source, err := filepath.Abs(ExpandPath(l.Source))
	if err != nil {
		log.Fatal(err)
	}

	path := ExpandPath(l.Path)

	// If the file exists and is a symlink, let's check the source is correct
	if _, err := os.Lstat(path); err == nil {
		src, err := os.Readlink(path)
		if err != nil {
			log.Fatal(err)
		}

		// If the source is not correct, let's delete the symlink
		if src != l.Source {
			if err := os.Remove(path); err != nil {
				log.Fatal(err)
			}
		} else {
			// Otherwise everything is as we want it, so return
			return &l
		}
	}

	// Perform the link. This will error if the file exists, and we'll
	// leave the safe behaviour in
	if err := os.Symlink(source, path); err != nil {
		log.Fatal(err)
	}

	return &l
}

// Delete deletes the symlink from the Path
func (l Link) Delete() *Link {
	log := newLogger("Link", "delete")
	l.satisfy(log)

	log.Info(l.Path)
	if Config.DryRun {
		return &l
	}

	path := ExpandPath(l.Path)

	if err := os.Remove(path); err != nil {
		log.Fatal(err)
	}

	return &l
}
