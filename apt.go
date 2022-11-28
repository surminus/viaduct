package viaduct

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Apt configures Ubuntu apt repositories. It will automatically use sudo
// if the user is not root.
type Apt struct {
	// Name is the name of the resource, and is what the file written to
	// disk will be based on
	Name string
	// URI is the source URI of the repository
	URI string

	// Distribution is normally the codename of the distribution. Defaults to
	// the Ubuntu codename.
	Distribution string
	// Source is repository type. Defaults to main.
	Source string
	// Parameters is a map of optional parameters that gets represented as key
	// value pairs, eg "[arch=amd64]"
	Parameters map[string]string

	// path is a private attribute for where to write the apt file
	path string
}

// satisfy sets default values for the parameters for a particular
// resource
func (a *Apt) satisfy(log *logger) {
	// Set required values here, and error if they are not set
	if a.Name == "" {
		log.Fatal("Required parameter: Name")
	}

	if a.URI == "" {
		log.Fatal("Required parameter: URI")
	}

	if !isRoot() {
		log.Fatal("Apt resource must be run as root")
	}

	// Set optional defaults here
	if a.Distribution == "" {
		a.Distribution = Attribute.Platform.UbuntuCodename
	}

	if a.Source == "" {
		a.Source = "main"
	}

	a.path = filepath.Join("/etc", "apt", "sources.list.d", fmt.Sprintf("%s.list", a.Name))
}

func NewAptUpdate() Apt {
	return Apt{}
}

// Update can be used in scripting mode to perform "apt-get update"
func (a Apt) Update() *Apt {
	log := newLogger("Apt", "update")
	err := a.updateApt(log)
	if err != nil {
		log.Fatal(err)
	}

	return &a
}

// Create can be used in scripting mode to create an apt repository
func (a Apt) Create() *Apt {
	log := newLogger("Apt", "create")
	err := a.createApt(log)
	if err != nil {
		log.Fatal(err)
	}

	return &a
}

// Delete can be used in scripting mode to delete an apt repository
func (a Apt) Delete() *Apt {
	log := newLogger("Apt", "delete")
	err := a.deleteApt(log)
	if err != nil {
		log.Fatal(err)
	}

	return &a
}

// AptUpdate is a helper function to perform "apt-get update"
// Should be converted to a proper resource
func (a Apt) updateApt(log *logger) error {
	if Config.DryRun {
		log.Info()
		return nil
	}

	if !isRoot() {
		return fmt.Errorf("must be run as root")
	}

	log.Info()

	command := []string{"apt-get", "update", "-y"}

	cmd := exec.Command("bash", "-c", strings.Join(command, " "))
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

// Create adds a new apt repository
func (a Apt) createApt(log *logger) error {
	a.satisfy(log)

	if Config.DryRun {
		log.Info(a.Name)
		return nil
	}

	content := []string{
		"deb",
	}

	if len(a.Parameters) > 0 {
		var params []string

		for k, v := range a.Parameters {
			params = append(params, fmt.Sprintf("%s=%s", k, v))
		}

		content = append(content, fmt.Sprintf("[%s]", strings.Join(params, " ")))
	}

	content = append(content, []string{
		a.URI,
		a.Distribution,
		a.Source,
		"\n",
	}...)

	if FileExists(a.path) {
		if con, err := os.ReadFile(a.path); err == nil {
			if string(con) == strings.Join(content, " ") {
				log.Noop(a.Name)
				return nil
			}
		} else {
			return err
		}
	}

	if err := os.WriteFile(a.path, []byte(strings.Join(content, " ")), 0o644); err != nil {
		return err
	}

	log.Info(a.Name)

	return nil
}

// Delete removes an apt repository
func (a Apt) deleteApt(log *logger) error {
	a.satisfy(log)

	if Config.DryRun {
		log.Info(a.Name)
		return nil
	}

	if !FileExists(a.path) {
		log.Noop(a.Name)
		return nil
	}

	if err := os.Remove(a.path); err != nil {
		return err
	}

	log.Info(a.Name)

	return nil
}
