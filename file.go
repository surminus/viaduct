package viaduct

import (
	"log"
)

// File manages files on the filesystem
type File struct {
	Path    string
	Content string
	Mode    string
	Owner   string
	Group   string
}

// satisfy sets default values for the parameters for a particular
// resource
func (f *File) satisfy() {
	// Set required values here, and error if they are not
	// set
	if f.Path == "" {
		log.Fatal("Path is a required parameter")
	}

	// Set optional defaults here, or leave them empty if that's required
	if f.Mode == "" {
		f.Mode = "0644"
	}

	if f.Owner == "" {
		f.Owner = "root"
	}

	if f.Group == "" {
		f.Group = "root"
	}
}

// Create creates or updates a file
func (f File) Create() (err error) {
	f.satisfy()

	return err
}

// Delete deletes a file
func (f *File) Delete() (err error) {
	return err
}
