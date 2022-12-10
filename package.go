package viaduct

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Package installs one or more packages. Use Name for a single package
// install, or Names for multi-package install.
type Package struct {
	// Name is the package name
	Name string

	// Names are the package names
	Names []string

	// Verbose displays output from STDOUT. Optional.
	Verbose bool
}

// satisfy sets default values for the parameters for a particular
// resource
func (p *Package) satisfy(log *logger) error {
	// Set required values here, and error if they are not set
	if p.Name == "" && len(p.Names) < 1 {
		return fmt.Errorf("Required parameter: Name / Names")
	}

	if !isRoot() {
		return fmt.Errorf("Package resource must be run as root")
	}

	// Set optional defaults here
	return nil
}

// P is shortcut for declaring a new Package resource
func P(name string) Package {
	return Package{
		Name: name,
	}
}

// Ps is a shortcut for declaring a new Package resource with multiple packages
func Ps(names ...string) Package {
	return Package{
		Names: names,
	}
}

// Create can be used in scripting mode to install a package
func (p Package) Create() *Package {
	log := newLogger("Package", "install")
	err := p.createPackage(log)
	if err != nil {
		log.Fatal(err)
	}

	return &p
}

// Delete can be used in scripting mode to delete a package
func (p Package) Delete() *Package {
	log := newLogger("Package", "delete")
	err := p.deletePackage(log)
	if err != nil {
		log.Fatal(err)
	}

	return &p
}

func (p Package) createPackage(log *logger) error {
	if err := p.satisfy(log); err != nil {
		return err
	}

	if p.Name != "" {
		p.Names = append(p.Names, p.Name)
	}

	log.Info("Packages:\n\t", strings.Join(p.Names, "\n\t"))
	if Config.DryRun {
		return nil
	}

	return installPkg(Attribute.Platform.ID, p.Names, p.Verbose)
}

// Delete uninstalls a package
func (p Package) deletePackage(log *logger) error {
	if err := p.satisfy(log); err != nil {
		return err
	}

	p.Names = append(p.Names, p.Name)

	log.Info("Packages:\n\t", strings.Join(p.Names, "\n\t"))
	if Config.DryRun {
		return nil
	}

	return removePkg(Attribute.Platform.ID, []string{p.Name}, p.Verbose)
}

func installPkg(platform string, pkgs []string, verbose bool) error {
	switch platform {
	case "debian", "ubuntu", "linuxmint":
		return aptGetCmd("install", pkgs, verbose)
	case "fedora", "centos":
		return dnfCmd("install", pkgs, verbose)
	case "arch", "manjaro":
		return pacmanCmd("-S", pkgs, verbose)
	default:
		return fmt.Errorf("unrecognised distribution: %s", Attribute.Platform.ID)
	}
}

func removePkg(platform string, pkgs []string, verbose bool) error {
	switch platform {
	case "debian", "ubuntu", "linuxmint":
		return aptGetCmd("remove", pkgs, verbose)
	case "fedora", "centos":
		return dnfCmd("remove", pkgs, verbose)
	case "arch":
		return pacmanCmd("-R", pkgs, verbose)
	default:
		return fmt.Errorf("unrecognised distribution: %s", Attribute.Platform.ID)
	}
}

func installCmd(args []string, verbose bool) (err error) {
	// nolint:gosec
	cmd := exec.Command(args[0], args[1:]...)

	if verbose {
		cmd.Stdout = os.Stdout
	}

	cmd.Stderr = os.Stderr

	err = cmd.Run()

	return err
}

func aptGetCmd(command string, packages []string, verbose bool) error {
	args := []string{"apt-get", command, "-y"}

	args = append(args, packages...)

	return installCmd(args, verbose)
}

func dnfCmd(command string, packages []string, verbose bool) error {
	args := []string{"dnf", command, "-y"}

	args = append(args, packages...)

	return installCmd(args, verbose)
}

func pacmanCmd(command string, packages []string, verbose bool) error {
	args := []string{"pacman", command, "--noconfirm"}

	if command == "-S" {
		args = append(args, "--needed")
	}

	args = append(args, packages...)

	return installCmd(args, verbose)
}
