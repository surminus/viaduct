package viaduct

import (
	"log"
	"os"
	"os/exec"
)

// Package installs a package
type Package struct {
	// Name is the package name
	Name string

	// Sudo runs the command with sudo
	Sudo bool
}

// Packages allow installation of multiple packages
type Packages struct {
	// Names are the package names
	Names []string

	// Sudo runs the command with sudo
	Sudo bool
}

// satisfy sets default values for the parameters for a particular
// resource
func (p *Package) satisfy() {
	// Set required values here, and error if they are not set
	if p.Name == "" {
		log.Fatal("==> Package [error] Required parameter: Name")
	}

	// Set optional defaults here
}

// satisfy sets default values for the parameters for a particular
// resource
func (p *Packages) satisfy() {
	// Set required values here, and error if they are not set
	if len(p.Names) < 1 {
		log.Fatal("==> Packages [error] At least one package required")
	}

	// Set optional defaults here
}

// Install installs a packages
func (p Package) Install() *Package {
	p.satisfy()

	log.Println("==> Package [install]")
	if Config.DryRun {
		return &p
	}

	installPkg(Attribute.Platform.ID, []string{p.Name}, p.Sudo)

	return &p
}

// Remove uninstalls a package
func (p Package) Remove() *Package {
	p.satisfy()

	log.Println("==> Package [remove]")
	if Config.DryRun {
		return &p
	}

	removePkg(Attribute.Platform.ID, []string{p.Name}, p.Sudo)

	return &p
}

// Install installs a packages
func (p Packages) Install() *Packages {
	p.satisfy()

	log.Println("==> Packages [install]")
	if Config.DryRun {
		return &p
	}

	installPkg(Attribute.Platform.ID, p.Names, p.Sudo)

	return &p
}

// Remove uninstalls a package
func (p Packages) Remove() *Packages {
	p.satisfy()

	log.Println("==> Packages [remove]")
	if Config.DryRun {
		return &p
	}

	removePkg(Attribute.Platform.ID, p.Names, p.Sudo)

	return &p
}

func installPkg(platform string, pkgs []string, sudo bool) {
	switch platform {
	case "debian", "ubuntu", "linuxmint":
		aptGetCmd("install", pkgs, sudo)
	case "fedora", "centos":
		dnfCmd("install", pkgs, sudo)
	case "arch":
		pacmanCmd("-S", pkgs, sudo)
	default:
		log.Fatal("Unrecognised distribution:", Attribute.Platform.ID)
	}

}

func removePkg(platform string, pkgs []string, sudo bool) {
	switch platform {
	case "debian", "ubuntu", "linuxmint":
		aptGetCmd("remove", pkgs, sudo)
	case "fedora", "centos":
		dnfCmd("remove", pkgs, sudo)
	case "arch":
		pacmanCmd("-R", pkgs, sudo)
	default:
		log.Fatal("Unrecognised distribution:", Attribute.Platform.ID)
	}
}

func installCmd(args []string) (err error) {
	cmd := exec.Command(args[0], args[1:]...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	return err
}

func aptGetCmd(command string, packages []string, sudo bool) {
	args := []string{"apt-get", command, "-y"}

	if sudo {
		args = prependSudo(args)
	}
	args = append(args, packages...)

	err := installCmd(args)
	if err != nil {
		log.Fatal(err)
	}
}

func dnfCmd(command string, packages []string, sudo bool) {
	args := []string{"dnf", command, "-y"}

	if sudo {
		args = prependSudo(args)
	}
	args = append(args, packages...)

	err := installCmd(args)
	if err != nil {
		log.Fatal(err)
	}
}

func pacmanCmd(command string, packages []string, sudo bool) {
	args := []string{"pacman", command, "--noconfirm"}

	if sudo {
		args = prependSudo(args)
	}
	args = append(args, packages...)

	err := installCmd(args)
	if err != nil {
		log.Fatal(err)
	}
}

func prependSudo(args []string) []string {
	return append([]string{"sudo"}, args...)
}
