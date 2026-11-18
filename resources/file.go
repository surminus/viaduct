package resources

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"text/template"

	"github.com/surminus/viaduct"
)

// File manages files on the filesystem
type File struct {
	// Path is the path of the file
	Path string
	// Content is the content of the file
	Content string
	// Source is a file to copy the content from, read when the resource
	// runs, so it can be one that an earlier resource creates. Set either
	// Content or Source.
	Source string
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

// CopyFile writes the content of source to path, reading it when the
// resource runs
func CopyFile(source, path string) *File {
	return &File{Path: path, Source: source}
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

func (f *File) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if f.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	if f.Source != "" {
		switch {
		case f.Content != "":
			return fmt.Errorf("cannot set both Content and Source")
		case f.Delete:
			return fmt.Errorf("cannot set both Delete and Source")
		case f.PermissionsOnly:
			return fmt.Errorf("cannot set both Source and PermissionsOnly")
		}
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

// EmbeddedFile returns the contents of an embedded file. Wrap it in
// viaduct.Must to use it inline.
func EmbeddedFile(files embed.FS, path string) (string, error) {
	out, err := files.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// NewTemplate renders an embedded Go template with the given variables. Wrap
// it in viaduct.Must to use it inline.
func NewTemplate(files embed.FS, path string, variables any) (string, error) {
	out, err := EmbeddedFile(files, path)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New(path).Parse(out)
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	if err := tmpl.Execute(&b, variables); err != nil {
		return "", err
	}

	return b.String(), nil
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

func (f *File) Run(ctx context.Context, log *viaduct.Logger) error {
	switch {
	case f.Delete:
		return f.deleteFile(ctx, log)
	case f.PermissionsOnly:
		return f.setPermissions(ctx, log)
	default:
		return f.createFile(ctx, log)
	}
}

// Create creates or updates a file
func (f *File) createFile(ctx context.Context, log *viaduct.Logger) error {
	content := f.Content

	if f.Source != "" {
		env := viaduct.EnvFrom(ctx)
		source := env.Path(viaduct.ExpandPath(f.Source))

		b, err := os.ReadFile(source)
		switch {
		case errors.Is(err, fs.ErrNotExist) && env.DryRun:
			// An earlier resource would have created it
			log.Info("created", "path", env.Path(viaduct.ExpandPath(f.Path)), "source", source)
			return nil
		case err != nil:
			return fmt.Errorf("reading Source: %w", err)
		}

		content = string(b)
	}

	return writeManagedFile(ctx, log, f.Path, content, &f.Permissions, f.CreateDirIfMissing)
}

// setPermissions applies the mode and ownership to a file that already exists,
// leaving its content alone
func (f *File) setPermissions(ctx context.Context, log *viaduct.Logger) error {
	path := viaduct.EnvFrom(ctx).Path(viaduct.ExpandPath(f.Path))

	// There is no content to fall back on, so a missing file is an error
	// rather than something to create. In a dry run, an earlier resource may
	// be what creates it
	if !viaduct.FileExists(path) {
		if viaduct.EnvFrom(ctx).DryRun {
			log.Info("permissions-managed", "path", path)
			return nil
		}

		return fmt.Errorf("file does not exist: %s", path)
	}

	return f.applyOnly(ctx, log, path, f.Mode)
}

// Delete deletes a file
func (f *File) deleteFile(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path := env.Path(viaduct.ExpandPath(f.Path))

	// If the file does not exist, return early
	if !viaduct.FileExists(path) {
		log.Noop("up-to-date", "path", path)
		return nil
	}

	if !change(ctx, log, "deleted", "path", path) {
		return nil
	}

	return os.Remove(path)
}
