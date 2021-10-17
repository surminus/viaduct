package viaduct

import (
	"io/ioutil"
	"log"
	"os"
)

// File manages files on the filesystem
type File struct {
	Path    string
	Content string
	Mode    os.FileMode
}

// satisfy sets default values for the parameters for a particular
// resource
func (f *File) satisfy() {
	// Set required values here, and error if they are not set
	if f.Path == "" {
		log.Fatal("Path is a required parameter")
	}

	// Set optional defaults here
	if f.Mode == 0 {
		f.Mode = 0644
	}
}

// Create creates or updates a file
func (f File) Create() *File {
	f.satisfy()

	err := ioutil.WriteFile(f.Path, []byte(f.Content), f.Mode)
	if err != nil {
		log.Fatal(err)
	}

	return &f
}

// Delete deletes a file
func (f File) Delete() *File {
	f.satisfy()

	err := os.Remove(f.Path)
	if err != nil {
		log.Fatal(err)
	}

	return &f
}
