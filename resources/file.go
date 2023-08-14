package resources

import (
	"bytes"
	"embed"
	"fmt"
	"log"
	"os"
	"strconv"
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
	// Mode is the permissions set of the file
	Mode os.FileMode
	// Root enforces using the root user
	Root bool
	// User sets the user permissions by user name
	User string
	// Group sets the group permissions by group name
	Group string
	// UID sets the user permissions by UID
	UID int
	// GID sets the group permissions by GID
	GID int
	// Delete will delete the file rather than create it if set to true.
	Delete bool
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

	if f.User == "" && f.UID == 0 && !f.Root {
		if uid, err := strconv.Atoi(viaduct.Attribute.User.Uid); err != nil {
			return err
		} else {
			f.UID = uid
		}
	}

	if f.Group == "" && f.GID == 0 && !f.Root {
		if gid, err := strconv.Atoi(viaduct.Attribute.User.Gid); err != nil {
			return err
		} else {
			f.GID = gid
		}
	}

	return nil
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

	return setFilePermissions(log,
		path,
		f.UID,
		f.GID,
		f.User,
		f.Group,
		f.Mode,
	)
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
