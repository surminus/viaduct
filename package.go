package viaduct

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Package installs one or more packages. Specify the package names.
type Package struct {
	// Names are the package names
	Names []string

	// Verbose displays output from STDOUT. Optional.
	Verbose bool

	// Uninstall will uninstall the specified packages.
	Uninstall bool
}

func (p *Package) opts() *ResourceOptions {
	return NewResourceOptionsWithLock()
}

// satisfy sets default values for the parameters for a particular
// resource
func (p *Package) satisfy(log *logger) error {
	// Set required values here, and error if they are not set
	if len(p.Names) < 1 {
		return fmt.Errorf("Required parameter: Names")
	}

	if !isRoot() {
		return fmt.Errorf("Package resource must be run as root")
	}

	// Set optional defaults here
	return nil
}

// P is shortcut for declaring a new Package resource
func Pkg(name string) *Package {
	return &Package{
		Names: []string{name},
	}
}

// Ps is a shortcut for declaring a new Package resource with multiple packages
func Pkgs(names ...string) *Package {
	return &Package{
		Names: names,
	}
}

func (p *Package) operationName() string {
	if p.Uninstall {
		return "Uninstall"
	}

	return "Install"
}

func (p *Package) run(log *logger) error {
	if p.Uninstall {
		return p.uninstall(log)
	} else {
		return p.install(log)
	}
}

func (p *Package) install(log *logger) error {
	log.Info("Packages:\n\t", strings.Join(p.Names, "\n\t"))
	if Config.DryRun {
		return nil
	}

	return installPkg(Attribute.Platform.ID, p.Names, p.Verbose)
}

func (p *Package) uninstall(log *logger) error {
	log.Info("Packages:\n\t", strings.Join(p.Names, "\n\t"))
	if Config.DryRun {
		return nil
	}

	return removePkg(Attribute.Platform.ID, p.Names, p.Verbose)
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
