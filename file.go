package viaduct

import (
	"bytes"
	"embed"
	"io/ioutil"
	"log"
	"os"
	"text/template"
	"time"
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

// EmbeddedFile is a small helper function to helper reading
// embedded files
func EmbeddedFile(files embed.FS, path string) string {
	out, err := files.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}

	return string(out)
}

// NewTemplate makes it easier to return the data required
// to parse a template
func NewTemplate(files embed.FS, path string, variables interface{}) string {
	out := EmbeddedFile(files, path)

	tmpl, err := template.New(time.Now().String()).Parse(string(out))
	if err != nil {
		log.Fatal(err)
	}

	var b bytes.Buffer
	err = tmpl.Execute(&b, variables)
	if err != nil {
		log.Fatal(err)
	}

	return b.String()
}

// Create creates or updates a file
func (f File) Create() *File {
	f.satisfy()

	log.Println("==> File [create]", f.Path)
	if Config.DryRun {
		return &f
	}

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
	if Config.DryRun {
		return &f
	}

	err := os.Remove(f.Path)
	if err != nil {
		log.Fatal(err)
	}

	return &f
}
