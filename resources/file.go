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
	// CreateDirIfMissing creates the parent directory if it does not already
	// exist, rather than relying on a separately declared Directory resource.
	// The parent is created with 0755 and default ownership.
	CreateDirIfMissing bool

	// PermissionsOnly manages the mode and ownership of a file whose content
	// belongs to something else, leaving that content alone. The file has to
	// exist already.
	//
	// Only what is set is applied: nothing is defaulted, so a Mode on its own
	// leaves ownership as it is, and a User on its own leaves the mode and the
	// group as they are.
	PermissionsOnly bool

	// Permissions manages permissions for the file
	Permissions
}

// Touch simply touches an empty file to disk
func Touch(path string) *File {
	return &File{Path: path}
}

// CreateFile writes content to the specified path
func CreateFile(path, content string) *File {
	return &File{Path: path, Content: content}
}

// CreateFileP writes content to the specified path, creating the parent
// directory if it does not already exist. It is the equivalent of running
// "mkdir -p" before writing the file.
func CreateFileP(path, content string) *File {
	return &File{Path: path, Content: content, CreateDirIfMissing: true}
}

// DeleteFile will delete the specified file
func DeleteFile(path string) *File {
	return &File{Path: path, Delete: true}
}

// SetPermissions manages the mode of an existing file without touching its
// content or its ownership. Set User, Group or the numeric equivalents on the
// resource itself to manage ownership too.
func SetPermissions(path string, mode os.FileMode) *File {
	return &File{Path: path, PermissionsOnly: true, Permissions: Permissions{Mode: mode}}
}

func (f *File) Description() string {
	return f.Path
}

func (f *File) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

func (f *File) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if f.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	if f.PermissionsOnly {
		if f.Content != "" {
			return fmt.Errorf("cannot set both Content and PermissionsOnly")
		}

		if f.Delete {
			return fmt.Errorf("cannot set both Delete and PermissionsOnly")
		}

		// Nothing is defaulted here, because defaulting the ownership would
		// mean taking ownership of a file the caller only asked to chmod
		if f.Mode == 0 && !f.managesOwnership() {
			return fmt.Errorf("PermissionsOnly needs one of Mode, User, Group, UID or GID")
		}

		return nil
	}

	if f.Mode == 0 {
		f.Mode = 0o644
	}

	return f.preflightPermissions(pfile)
}

// managesOwnership reports whether any ownership was asked for
func (f *File) managesOwnership() bool {
	return f.User != "" || f.Group != "" || f.UID != 0 || f.GID != 0
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
	switch {
	case f.Delete:
		return "Delete"
	case f.PermissionsOnly:
		return "Update"
	default:
		return "Create"
	}
}

func (f *File) Run(log *viaduct.Logger) error {
	switch {
	case f.Delete:
		return f.deleteFile(log)
	case f.PermissionsOnly:
		return f.setPermissions(log)
	default:
		return f.createFile(log)
	}
}

// Create creates or updates a file
func (f *File) createFile(log *viaduct.Logger) error {
	return writeManagedFile(log, f.Path, f.Content, &f.Permissions, f.CreateDirIfMissing)
}

// setPermissions applies the mode and ownership to a file that already exists,
// leaving its content alone
func (f *File) setPermissions(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(f.Path)

	if viaduct.Cli.DryRun {
		log.Info("permissions-managed", "path", path)
		return nil
	}

	// There is no content to fall back on, so a missing file is an error
	// rather than something to create
	if !viaduct.FileExists(path) {
		return fmt.Errorf("file does not exist: %s", path)
	}

	if f.managesOwnership() {
		uid, gid, err := f.resolveOwnership()
		if err != nil {
			return err
		}

		// Whichever side was not asked for keeps the owner it has, so setting
		// a User does not hand the group to root
		current, err := fileOwnership(path)
		if err != nil {
			return err
		}

		if f.User == "" && f.UID == 0 {
			uid = current.uid
		}

		if f.Group == "" && f.GID == 0 {
			gid = current.gid
		}

		if err := applyChown(log, path, uid, gid); err != nil {
			return err
		}
	}

	if f.Mode != 0 {
		return applyChmod(log, path, f.Mode)
	}

	return nil
}

// Delete deletes a file
func (f *File) deleteFile(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(f.Path)

	if viaduct.Cli.DryRun {
		log.Info("deleted", "path", path)
		return nil
	}

	// If the file does not exist, return early
	if !viaduct.FileExists(path) {
		log.Noop("up-to-date", "path", path)
		return nil
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	log.Info("deleted", "path", path)

	return nil
}
