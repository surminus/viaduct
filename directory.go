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
		log.Fatal("==> Directory [error] Required parameter: Path")
	}

	// Set optional defaults here
	if d.Mode == 0 {
		d.Mode = 0o755
	}
}

// Create creates a directory
func (d Directory) Create() *Directory {
	d.satisfy()

	log.Println("==> Directory [create]", d.Path)
	if Config.DryRun {
		return &d
	}

	err := os.MkdirAll(d.Path, d.Mode)
	if err != nil {
		log.Fatal(err)
	}

	return &d
}

// Delete deletes a directory.
func (d Directory) Delete() *Directory {
	d.satisfy()

	log.Println("==> Directory [delete]", d.Path)

	if Config.DryRun {
		return &d
	}

	if err := os.RemoveAll(d.Path); err != nil {
		log.Fatal(err)
	}

	return &d
}
