package viaduct

import (
	"fmt"
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

	// The source should always be the full path, so we will
	// attempt to expand it
	source, err := filepath.Abs(ExpandPath(l.Source))
	if err != nil {
		log.Fatal(err)
	}

	path := ExpandPath(l.Path)
	logmsg := fmt.Sprintf("%s -> %s", source, path)

	if Config.DryRun {
		log.Info(logmsg)
		return &l
	}

	// If the file exists and is a symlink, let's check the source is correct
	if _, err := os.Lstat(path); err == nil {
		src, err := os.Readlink(path)
		if err != nil {
			log.Fatal(err)
		}

		// If the source is not correct, let's delete the symlink
		if src != source {
			if err := os.Remove(path); err != nil {
				log.Fatal(err)
			}
		} else {
			// Otherwise everything is as we want it, so return
			log.Noop(logmsg)
			return &l
		}
	}

	if err := os.Symlink(source, path); err != nil {
		log.Fatal(err)
	}

	log.Info(logmsg)

	return &l
}

// Delete deletes the symlink from the Path
func (l Link) Delete() *Link {
	log := newLogger("Link", "delete")
	l.satisfy(log)

	path := ExpandPath(l.Path)

	if Config.DryRun {
		log.Info(path)
		return &l
	}

	if !FileExists(path) {
		log.Noop(path)
		return &l
	}

	if err := os.Remove(path); err != nil {
		log.Fatal(err)
	}

	log.Info(path)

	return &l
}
