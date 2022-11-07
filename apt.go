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

// AptUpdate is a helper function to perform "apt-get update" and will
// automatically run using sudo if the user is not root
func AptUpdate() {
	log := newLogger("Apt", "update")

	if !isRoot() {
		log.Fatal("Must be run as root")
	}

	log.Info()

	command := []string{"apt-get", "update", "-y"}

	cmd := exec.Command("bash", "-c", strings.Join(command, " "))
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

// Add adds a new apt repository
func (a Apt) Add() *Apt {
	log := newLogger("Apt", "add")
	a.satisfy(log)

	if Config.DryRun {
		log.Info(a.Name)
		return &a
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
				return &a
			}
		} else {
			log.Fatal(err)
		}
	}

	if err := os.WriteFile(a.path, []byte(strings.Join(content, " ")), 0o644); err != nil {
		log.Fatal(err)
	}

	log.Info(a.Name)

	return &a
}

// Add removes an apt repository
func (a Apt) Remove() *Apt {
	log := newLogger("Apt", "remove")
	a.satisfy(log)

	if Config.DryRun {
		log.Info(a.Name)
		return &a
	}

	if !FileExists(a.path) {
		log.Noop(a.Name)
		return &a
	}

	if err := os.Remove(a.path); err != nil {
		log.Fatal(err)
	}

	log.Info(a.Name)

	return &a
}
