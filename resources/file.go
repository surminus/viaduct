package resources

import (
	"bytes"
	"embed"
	"fmt"
	"log"
	"os"
	"text/template"
	"time"

	"github.com/surminus/viaduct"
)

// File manages files on the filesystem
type File struct {
	// Path is the path of the file
	Path string
	// Content is the content of the file
	Content string
	// Delete will delete the file rather than create it if set to true.
	Delete bool

	permissions
}

// Touch simply touches an empty file to disk
func Touch(path string) *File {
	return &File{Path: path}
}

// CreateFile writes content to the specified path
func CreateFile(path, content string) *File {
	return &File{Path: path, Content: content}
}

// DeleteFile will delete the specified file
func DeleteFile(path string) *File {
	return &File{Path: path, Delete: true}
}

func (f *File) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

func (f *File) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if f.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	if f.Mode == 0 {
		f.Mode = 0o644
	}

	return f.preflightPermissions(pfile)
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

	tmpl, err := template.New(time.Now().String()).Parse(out)
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

func (f *File) OperationName() string {
	if f.Delete {
		return "Delete"
	}

	return "Create"
}

func (f *File) Run(log *viaduct.Logger) error {
	if f.Delete {
		return f.deleteFile(log)
	} else {
		return f.createFile(log)
	}
}

// Create creates or updates a file
func (f *File) createFile(log *viaduct.Logger) error {
	if viaduct.Config.DryRun {
		log.Info(f.Path)
		return nil
	}

	path := viaduct.ExpandPath(f.Path)

	var shouldWriteFile bool
	if viaduct.FileExists(path) {
		if content, err := os.ReadFile(path); err == nil {
			if string(content) != f.Content {
				shouldWriteFile = true
			}
		} else {
			return err
		}
	} else {
		shouldWriteFile = true
	}

	// If we want to run it as sudo, then we create a temporary file, write
	// the content, and then copy the file into place.
	if shouldWriteFile {
		err := os.WriteFile(path, []byte(f.Content), f.Mode)
		if err != nil {
			return err
		}
		log.Info(path)
	} else {
		log.Noop(path)
	}

	return f.setFilePermissions(log, path)
}

// Delete deletes a file
func (f *File) deleteFile(log *viaduct.Logger) error {
	if viaduct.Config.DryRun {
		log.Info(f.Path)
		return nil
	}

	path := viaduct.ExpandPath(f.Path)

	// If the file does not exist, return early
	if !viaduct.FileExists(path) {
		log.Noop(f.Path)
		return nil
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	log.Info(f.Path)

	return nil
}
