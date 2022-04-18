package viaduct

import (
	"log"
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
func (l *Link) satisfy() {
	// Set required values here, and error if they are not set
	if l.Path == "" {
		log.Fatal("==> Link [error] Required parameter: Path")
	}

	// Set optional defaults here
}

// Create creates a new symlink from Source to Path
func (l Link) Create() *Link {
	l.satisfy()

	// We must specify the source if we are performing a link
	if l.Source == "" {
		log.Fatal("==> Link [error] Required parameter: Source")
	}

	log.Printf("==> Link [create] %s -> %s", l.Path, l.Source)
	if Config.DryRun {
		return &l
	}

	// The source should always be the full path, so we will
	// attempt to expand it
	source, err := filepath.Abs(l.Source)
	if err != nil {
		log.Fatal(err)
	}

	// If the file exists and is a symlink, let's check the source is correct
	if _, err := os.Lstat(l.Path); err == nil {
		src, err := os.Readlink(l.Path)
		if err != nil {
			log.Fatal(err)
		}

		// If the source is not correct, let's delete the symlink
		if src != l.Source {
			if err := os.Remove(l.Path); err != nil {
				log.Fatal(err)
			}
		}
	}

	// Perform the link. This will error if the file exists, and we'll
	// leave the safe behaviour in
	if err := os.Symlink(source, l.Path); err != nil {
		log.Fatal(err)
	}

	return &l
}

// Delete deletes the symlink from the Path
func (l Link) Delete() *Link {
	l.satisfy()

	log.Println("==> Link [delete]", l.Path)
	if Config.DryRun {
		return &l
	}

	if err := os.Remove(l.Path); err != nil {
		log.Fatal(err)
	}

	return &l
}
