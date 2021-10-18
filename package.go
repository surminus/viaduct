package viaduct

import (
	"log"
	"os"
	"os/exec"
)

// Package installs a package
type Package struct {
	Name string
}

// Packages allow installation of multiple packages
type Packages struct {
	Packages []*Package
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

// Install installs one or more packages
func (p Package) Install() *Package {
	p.satisfy()

	log.Println("==> Package [install]")
	if Config.DryRun {
		return &p
	}

	pkg := p.Name

	switch Attribute.Platform.ID {
	case "debian", "ubuntu", "linuxmint":
		aptGetCmd("install", []string{pkg})
	case "fedora", "centos":
		dnfCmd("install", []string{pkg})
	case "arch":
		pacmanCmd("-S", []string{pkg})
	default:
		log.Fatal("Unrecognised distribution:", Attribute.Platform.ID)
	}

	return &p
}

// Remove uninstalls a package
func (p Package) Remove() *Package {
	p.satisfy()

	log.Println("==> Package [remove]")
	if Config.DryRun {
		return &p
	}

	pkg := p.Name

	switch Attribute.Platform.ID {
	case "debian", "ubuntu", "linuxmint":
		aptGetCmd("remove", []string{pkg})
	case "fedora", "centos":
		dnfCmd("remove", []string{pkg})
	case "arch":
		pacmanCmd("-R", []string{pkg})
	default:
		log.Fatal("Unrecognised distribution:", Attribute.Platform.ID)
	}
	return &p
}

func installCmd(args []string) (err error) {
	cmd := exec.Command(args[0], args[1:]...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	return err
}

func aptGetCmd(command string, packages []string) {
	args := []string{"apt-get", command, "-y"}
	args = append(args, packages...)

	err := installCmd(args)
	if err != nil {
		log.Fatal(err)
	}
}

func dnfCmd(command string, packages []string) {
	args := []string{"dnf", command, "-y"}
	args = append(args, packages...)

	err := installCmd(args)
	if err != nil {
		log.Fatal(err)
	}
}

func pacmanCmd(command string, packages []string) {
	args := []string{"pacman", command, "--noconfirm"}
	args = append(args, packages...)

	err := installCmd(args)
	if err != nil {
		log.Fatal(err)
	}
}
