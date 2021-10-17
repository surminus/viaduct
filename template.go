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

// Template creates a file using the template package
type Template struct {
	Path      string
	Content   string
	Variables interface{}
	Mode      os.FileMode
}

// satisfy sets default values for the parameters for a particular
// resource
func (t *Template) satisfy() {
	// Set required values here, and error if they are not set
	if t.Path == "" {
		log.Fatal("==> Template [error] Required parameter: Path")
	}

	if t.Content == "" {
		log.Fatal("==> Template [error] Required parameter: Content")
	}

	// Set optional defaults here
	if t.Mode == 0 {
		t.Mode = 0644
	}
}

// NewTemplate makes it easier to return the data required
// to parse a template
func NewTemplate(files embed.FS, path string) string {
	out, err := files.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}

	return string(out)
}

// Create creates or updates a template
func (t Template) Create() *Template {
	t.satisfy()

	log.Println("==> Template [create]", t.Path)
	if Config.DryRun {
		return &t
	}

	tmpl, err := template.New(time.Now().String()).Parse(t.Content)
	if err != nil {
		log.Fatal(err)
	}

	var b bytes.Buffer
	err = tmpl.Execute(&b, t.Variables)
	if err != nil {
		log.Fatal(err)
	}

	err = ioutil.WriteFile(t.Path, b.Bytes(), t.Mode)
	if err != nil {
		log.Fatal(err)
	}

	return &t
}

// Delete deletes a template
func (t Template) Delete() *Template {
	t.satisfy()

	log.Println("==> Template [delete]", t.Path)
	if Config.DryRun {
		return &t
	}

	err := os.Remove(t.Path)
	if err != nil {
		log.Fatal(err)
	}

	return &t
}
