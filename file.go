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
		log.Fatal("==> File [error] Required parameter: Path")
	}

	// Set optional defaults here
	if f.Mode == 0 {
		f.Mode = 0644
	}
}

// Create creates or updates a file
func (f File) Create() *File {
	f.satisfy()

	log.Println("==> File [create]", f.Path)
	err := ioutil.WriteFile(f.Path, []byte(f.Content), f.Mode)
	if err != nil {
		log.Fatal(err)
	}

	return &f
}

// Delete deletes a file
func (f File) Delete() *File {
	f.satisfy()

	log.Println("==> File [delete]", f.Path)
	err := os.Remove(f.Path)
	if err != nil {
		log.Fatal(err)
	}

	return &f
}
