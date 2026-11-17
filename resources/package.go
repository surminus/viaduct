package resources

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/surminus/viaduct"
)

// Package installs one or more packages. Specify the package names. It has
// no timeout by default; WithTimeout and the ResourceTimeout option still
// apply.
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

	// Hold marks the specified packages as held back, so the package manager
	// will not upgrade them, like "apt-mark hold". It does not install them:
	// chain this after a Pkg if the packages need installing too. Only
	// supported on Debian derivatives.
	Hold bool

	// Unhold releases a hold, letting the packages be upgraded again.
	Unhold bool
}

func (p *Package) Description() string {
	return strings.Join(p.Names, ", ")
}

func (p *Package) Params() *viaduct.ResourceParams {
	// Package managers take their own lock, and their maintainer scripts write
	// the passwd database, so the package lock covers both. A timeout kills
	// the process, which can leave dpkg interrupted, so there is none.
	params := viaduct.NewResourceParamsWithLockKey(viaduct.PackageLock)
	params.Timeout = noTimeout

	return params
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (p *Package) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if len(p.Names) < 1 {
		return fmt.Errorf("required parameter: Names")
	}

	if err := requireRoot(ctx, "package"); err != nil {
		return err
	}

	if p.Purge && !purgeSupported(viaduct.Attributes().Platform.ID) {
		return fmt.Errorf("purge is not supported on %s", viaduct.Attributes().Platform.ID)
	}

	if p.Hold && p.Unhold {
		return fmt.Errorf("cannot set both Hold and Unhold")
	}

	if (p.Hold || p.Unhold) && (p.Uninstall || p.Purge) {
		return fmt.Errorf("cannot hold and uninstall the same packages")
	}

	if (p.Hold || p.Unhold) && !holdSupported(viaduct.Attributes().Platform.ID) {
		return fmt.Errorf("holding packages is not supported on %s", viaduct.Attributes().Platform.ID)
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

// holdSupported reports whether the platform has a package hold that Viaduct
// knows how to manage. dnf needs the versionlock plugin and pacman holds live
// in IgnorePkg in pacman.conf, so neither is covered yet.
func holdSupported(platform string) bool {
	switch platform {
	case "debian", "ubuntu", "linuxmint":
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

// HoldPkg is a shortcut for holding a package back from upgrades
func HoldPkg(name string) *Package {
	return &Package{
		Names: []string{name},
		Hold:  true,
	}
}

// HoldPkgs is a shortcut for holding multiple packages back from upgrades
func HoldPkgs(names ...string) *Package {
	return &Package{
		Names: names,
		Hold:  true,
	}
}

// UnholdPkg is a shortcut for releasing a held package
func UnholdPkg(name string) *Package {
	return &Package{
		Names:  []string{name},
		Unhold: true,
	}
}

// UnholdPkgs is a shortcut for releasing held packages
func UnholdPkgs(names ...string) *Package {
	return &Package{
		Names:  names,
		Unhold: true,
	}
}

func (p *Package) OperationName() string {
	switch {
	case p.Hold:
		return "Hold"
	case p.Unhold:
		return "Unhold"
	case p.Purge:
		return "Purge"
	case p.Uninstall:
		return "Uninstall"
	default:
		return "Install"
	}
}

func (p *Package) Run(ctx context.Context, log *viaduct.Logger) error {
	switch {
	case p.Hold, p.Unhold:
		return p.hold(ctx, log)
	case p.Uninstall, p.Purge:
		return p.uninstall(ctx, log)
	default:
		return p.install(ctx, log)
	}
}

func (p *Package) install(ctx context.Context, log *viaduct.Logger) error {
	args, err := installArgs(viaduct.Attributes().Platform.ID, p.Names)
	if err != nil {
		return err
	}

	if !change(ctx, log, "installing", "packages", strings.Join(p.Names, ", ")) {
		return nil
	}

	return runPkgCmd(ctx, log, args, p.Verbose)
}

func (p *Package) uninstall(ctx context.Context, log *viaduct.Logger) error {
	args, err := removeArgs(viaduct.Attributes().Platform.ID, p.Names, p.Purge)
	if err != nil {
		return err
	}

	msg := "uninstalling"
	if p.Purge {
		msg = "purging"
	}

	if !change(ctx, log, msg, "packages", strings.Join(p.Names, ", ")) {
		return nil
	}

	return runPkgCmd(ctx, log, args, p.Verbose)
}

// hold marks packages as held back, or releases them, leaving alone any that
// are already in the state we want
func (p *Package) hold(ctx context.Context, log *viaduct.Logger) error {
	held, err := heldPackages(ctx, log)
	if err != nil {
		return err
	}

	change := holdsToChange(p.Names, held, p.Hold)
	if len(change) == 0 {
		log.Noop("up-to-date", "packages", strings.Join(p.Names, ", "))
		return nil
	}

	action := "hold"
	if p.Unhold {
		action = "unhold"
	}

	log.Info(action, "packages", strings.Join(change, ", "))

	if viaduct.EnvFrom(ctx).DryRun {
		return nil
	}

	return runPkgCmd(ctx, log, append([]string{"apt-mark", action}, change...), p.Verbose)
}

// holdsToChange returns the packages whose hold state does not match what was
// asked for
func holdsToChange(names []string, held map[string]bool, want bool) []string {
	var change []string

	for _, name := range names {
		if held[name] != want {
			change = append(change, name)
		}
	}

	return change
}

// heldPackages returns the set of packages currently marked as held back. It
// runs in a dry run too.
func heldPackages(ctx context.Context, log *viaduct.Logger) (map[string]bool, error) {
	out, err := viaduct.Command{Name: "apt-mark", Args: []string{"showhold"}}.Output(ctx, log)
	if err != nil {
		return nil, fmt.Errorf("apt-mark showhold failed: %w", err)
	}

	held := make(map[string]bool)
	for _, name := range strings.Fields(string(out)) {
		held[name] = true
	}

	return held, nil
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

// runPkgCmd runs a package manager command, logging its stderr, and its stdout
// when verbose
func runPkgCmd(ctx context.Context, log *viaduct.Logger, args []string, verbose bool) error {
	c := viaduct.Command{Name: args[0], Args: args[1:]}

	if !verbose {
		c.Stdout = io.Discard
	}

	return c.Run(ctx, log)
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
