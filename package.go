package viaduct

import (
	"log"
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
func (p *Package) satisfy(log *logger) {
	// Set required values here, and error if they are not set
	if p.Name == "" && len(p.Names) < 1 {
		log.Fatal("Required parameter: Name / Names")
	}

	if !isRoot() {
		log.Fatal("Package resource must be run as root")
	}

	// Set optional defaults here
}

// Install installs a packages
func (p Package) Install() *Package {
	log := newLogger("Package", "install")
	p.satisfy(log)

	if p.Name != "" {
		p.Names = append(p.Names, p.Name)
	}

	log.Info("Packages:\n\t", strings.Join(p.Names, "\n\t"))
	if Config.DryRun {
		return &p
	}

	installPkg(Attribute.Platform.ID, p.Names, p.Verbose)

	return &p
}

// Remove uninstalls a package
func (p Package) Remove() *Package {
	log := newLogger("Package", "remove")
	p.satisfy(log)

	p.Names = append(p.Names, p.Name)

	log.Info("Packages:\n\t", strings.Join(p.Names, "\n\t"))
	if Config.DryRun {
		return &p
	}

	removePkg(Attribute.Platform.ID, []string{p.Name}, p.Verbose)

	return &p
}

func installPkg(platform string, pkgs []string, verbose bool) {
	switch platform {
	case "debian", "ubuntu", "linuxmint":
		aptGetCmd("install", pkgs, verbose)
	case "fedora", "centos":
		dnfCmd("install", pkgs, verbose)
	case "arch", "manjaro":
		pacmanCmd("-S", pkgs, verbose)
	default:
		log.Fatal("Unrecognised distribution:", Attribute.Platform.ID)
	}
}

func removePkg(platform string, pkgs []string, verbose bool) {
	switch platform {
	case "debian", "ubuntu", "linuxmint":
		aptGetCmd("remove", pkgs, verbose)
	case "fedora", "centos":
		dnfCmd("remove", pkgs, verbose)
	case "arch":
		pacmanCmd("-R", pkgs, verbose)
	default:
		log.Fatal("Unrecognised distribution:", Attribute.Platform.ID)
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

func aptGetCmd(command string, packages []string, verbose bool) {
	args := []string{"apt-get", command, "-y"}

	args = append(args, packages...)

	if err := installCmd(args, verbose); err != nil {
		log.Fatal(err)
	}
}

func dnfCmd(command string, packages []string, verbose bool) {
	args := []string{"dnf", command, "-y"}

	args = append(args, packages...)

	if err := installCmd(args, verbose); err != nil {
		log.Fatal(err)
	}
}

func pacmanCmd(command string, packages []string, verbose bool) {
	args := []string{"pacman", command, "--noconfirm"}

	if command == "-S" {
		args = append(args, "--needed")
	}

	args = append(args, packages...)

	if err := installCmd(args, verbose); err != nil {
		log.Fatal(err)
	}
}
