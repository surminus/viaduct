package resources

import (
	"fmt"
	"path/filepath"

	"github.com/surminus/viaduct"
)

// Deb will install a debian package from a remote URL
type Deb struct {
	// Name is the name of the package
	Name string
	// URI is the source URI of the package
	URI string

	// CheckSum will ensure the package is legitimate
	CheckSum string
	// Delete will remove the package from the system
	Delete bool
}

// Params allows the resource to dynamically set options that will be passed
// at compile time
func (d *Deb) Params() *viaduct.ResourceParams {
	return &viaduct.ResourceParams{GlobalLock: true}
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (d *Deb) PreflightChecks(log *viaduct.Logger) error {
	if d.Name == "" {
		return fmt.Errorf("Required parameter: Name")
	}

	if d.URI == "" {
		return fmt.Errorf("Required parameter: URI")
	}

	if !viaduct.IsRoot() {
		return fmt.Errorf("Deb resource must be run as root")
	}

	// Set optional defaults here

	return nil
}

func (d *Deb) OperationName() string {
	if d.Delete {
		return "Delete"
	}

	return "Create"
}

func (d *Deb) Run(log *viaduct.Logger) error {
	if d.Delete {
		return d.delete(log)
	} else {
		return d.create(log)
	}
}

func (d *Deb) create(log *viaduct.Logger) error {
	if viaduct.Cli.DryRun {
		log.Info()
		return nil
	}

	// Download package

	// Install package, if it's not already installed at that version

	return nil
}

func (d *Deb) delete(log *viaduct.Logger) error {
	if viaduct.Cli.DryRun {
		log.Info()
		return nil
	}

	return nil
}
