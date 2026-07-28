package resources

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/surminus/viaduct"
)

// Template renders a Go template file from disk and writes the result to
// the destination. For templates embedded into the configuration binary,
// see NewTemplate.
type Template struct {
	// Source is the path to the template file
	Source string

	// Dest is where to write the rendered file
	Dest string

	// Variables are made available to the template. Referencing a
	// variable that does not exist in this map is an error.
	Variables map[string]string

	// CreateDirIfMissing creates the parent directory of Dest if it does not
	// already exist. The parent is created with 0755 and default ownership.
	CreateDirIfMissing bool

	// Permissions manages permissions for the rendered file
	Permissions
}

func (t *Template) Description() string {
	return fmt.Sprintf("%s -> %s", t.Source, t.Dest)
}

func (t *Template) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (t *Template) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if t.Source == "" {
		return fmt.Errorf("required parameter: Source")
	}

	if t.Dest == "" {
		return fmt.Errorf("required parameter: Dest")
	}

	if t.Mode == 0 {
		t.Mode = 0o644
	}

	return t.preflightPermissions(pfile)
}

func (t *Template) OperationName() string {
	return "Create"
}

func (t *Template) Run(log *viaduct.Logger) error {
	if viaduct.Cli.DryRun {
		if t.CreateDirIfMissing {
			if err := ensureParentDir(log, viaduct.ExpandPath(t.Dest)); err != nil {
				return err
			}
		}

		log.Info("created", "path", viaduct.ExpandPath(t.Dest))
		return nil
	}

	source := viaduct.ExpandPath(t.Source)
	if !viaduct.FileExists(source) {
		return fmt.Errorf("source template does not exist: %s", source)
	}

	content, err := t.render(source)
	if err != nil {
		return err
	}

	// Writing goes through the same helper as the File resource, so a
	// rendered template gets the same content comparison and permission
	// handling
	return writeManagedFile(log, t.Dest, content, &t.Permissions, t.CreateDirIfMissing)
}

func (t *Template) render(source string) (string, error) {
	content, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New(filepath.Base(source)).Option("missingkey=error").Parse(string(content))
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	if err := tmpl.Execute(&b, t.Variables); err != nil {
		return "", err
	}

	return b.String(), nil
}
