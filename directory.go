package viaduct

import (
	"log"
	"os"
)

// Directory manages a directory on the filesystem
type Directory struct {
	Path string
	Mode os.FileMode
}

// satisfy sets default values for the parameters for a particular
// resource
func (d *Directory) satisfy() {
	// Set required values here, and error if they are not set
	if d.Path == "" {
		log.Fatal("Path is a required parameter")
	}

	// Set optional defaults here
	if d.Mode == 0 {
		d.Mode = 0755
	}
}

// Create creates a directory
func (d Directory) Create() *Directory {
	d.satisfy()

	err := os.MkdirAll(d.Path, d.Mode)
	if err != nil {
		log.Fatal(err)
	}

	return &d
}

// Delete deletes a directory
func (d Directory) Delete() *Directory {
	d.satisfy()

	err := os.RemoveAll(d.Path)
	if err != nil {
		log.Fatal(err)
	}

	return &d
}
