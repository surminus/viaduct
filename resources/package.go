package resources

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/surminus/viaduct"
)

// Package installs one or more packages. Specify the package names.
type Package struct {
	// Names are the package names
	Names []string

	// Verbose displays output from STDOUT. Optional.
	Verbose bool

	// Uninstall will uninstall the specified packages.
	Uninstall bool

	// Purge uninstalls the specified packages and removes their
	// configuration, like "apt-get purge". Not every package manager has an
	// equivalent, so this is only supported on Debian and Arch derivatives.
	Purge bool
}

func (p *Package) Description() string {
	return strings.Join(p.Names, ", ")
}

func (p *Package) Params() *viaduct.ResourceParams {
	// Package managers take their own lock, and their maintainer scripts write
	// the passwd database, so the package lock covers both
	return viaduct.NewResourceParamsWithLockKey(viaduct.PackageLock)
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (p *Package) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if len(p.Names) < 1 {
		return fmt.Errorf("required parameter: Names")
	}

	if !viaduct.IsRoot() {
		return fmt.Errorf("package resource must be run as root")
	}

	if p.Purge && !purgeSupported(viaduct.Attribute.Platform.ID) {
		return fmt.Errorf("purge is not supported on %s", viaduct.Attribute.Platform.ID)
	}

	// Set optional defaults here
	return nil
}

// purgeSupported reports whether the platform's package manager can remove a
// package's configuration along with the package itself
func purgeSupported(platform string) bool {
	switch platform {
	case "debian", "ubuntu", "linuxmint", "arch", "manjaro":
		return true
	default:
		return false
	}
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

// PurgePkg is a shortcut for purging a package and its configuration
func PurgePkg(name string) *Package {
	return &Package{
		Names: []string{name},
		Purge: true,
	}
}

// PurgePkgs is a shortcut for purging multiple packages and their
// configuration
func PurgePkgs(names ...string) *Package {
	return &Package{
		Names: names,
		Purge: true,
	}
}

func (p *Package) OperationName() string {
	switch {
	case p.Purge:
		return "Purge"
	case p.Uninstall:
		return "Uninstall"
	default:
		return "Install"
	}
}

func (p *Package) Run(log *viaduct.Logger) error {
	if p.Uninstall || p.Purge {
		return p.uninstall(log)
	}

	return p.install(log)
}

func (p *Package) install(log *viaduct.Logger) error {
	log.Info("installing", "packages", strings.Join(p.Names, ", "))
	if viaduct.Cli.DryRun {
		return nil
	}

	return installPkg(viaduct.Attribute.Platform.ID, p.Names, p.Verbose)
}

func (p *Package) uninstall(log *viaduct.Logger) error {
	if p.Purge {
		log.Info("purging", "packages", strings.Join(p.Names, ", "))
	} else {
		log.Info("uninstalling", "packages", strings.Join(p.Names, ", "))
	}

	if viaduct.Cli.DryRun {
		return nil
	}

	return removePkg(viaduct.Attribute.Platform.ID, p.Names, p.Verbose, p.Purge)
}

func installPkg(platform string, pkgs []string, verbose bool) error {
	args, err := installArgs(platform, pkgs)
	if err != nil {
		return err
	}

	return runPkgCmd(args, verbose)
}

func removePkg(platform string, pkgs []string, verbose, purge bool) error {
	args, err := removeArgs(platform, pkgs, purge)
	if err != nil {
		return err
	}

	return runPkgCmd(args, verbose)
}

// installArgs builds the command that installs packages on a platform
func installArgs(platform string, pkgs []string) ([]string, error) {
	switch platform {
	case "debian", "ubuntu", "linuxmint":
		return aptGetArgs("install", pkgs), nil
	case "fedora", "centos":
		return dnfArgs("install", pkgs), nil
	case "arch", "manjaro":
		return pacmanArgs("-S", pkgs), nil
	default:
		return nil, fmt.Errorf("unrecognised distribution: %s", platform)
	}
}

// removeArgs builds the command that removes packages on a platform, purging
// their configuration too when asked
func removeArgs(platform string, pkgs []string, purge bool) ([]string, error) {
	switch platform {
	case "debian", "ubuntu", "linuxmint":
		if purge {
			return aptGetArgs("purge", pkgs), nil
		}

		return aptGetArgs("remove", pkgs), nil
	case "fedora", "centos":
		return dnfArgs("remove", pkgs), nil
	case "arch", "manjaro":
		// -n drops the configuration rather than leaving .pacsave files
		if purge {
			return pacmanArgs("-Rn", pkgs), nil
		}

		return pacmanArgs("-R", pkgs), nil
	default:
		return nil, fmt.Errorf("unrecognised distribution: %s", platform)
	}
}

func runPkgCmd(args []string, verbose bool) (err error) {
	// nolint:gosec
	cmd := exec.Command(args[0], args[1:]...)

	if viaduct.Cli.JSON {
		cmd.Stdout = nil
		cmd.Stderr = nil
	} else {
		if verbose {
			cmd.Stdout = os.Stdout
		}
		cmd.Stderr = os.Stderr
	}

	err = cmd.Run()

	return err
}

func aptGetArgs(command string, packages []string) []string {
	args := []string{"apt-get", command, "-y"}

	return append(args, packages...)
}

func dnfArgs(command string, packages []string) []string {
	args := []string{"dnf", command, "-y"}

	return append(args, packages...)
}

func pacmanArgs(command string, packages []string) []string {
	args := []string{"pacman", command, "--noconfirm"}

	if command == "-S" {
		args = append(args, "--needed")
	}

	return append(args, packages...)
}
