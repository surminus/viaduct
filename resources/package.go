package resources

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/surminus/viaduct"
)

// Package installs one or more packages. Specify the package names. Only the
// packages that are not installed are passed to the package manager, and
// uninstalling or purging only touches the ones that are there. A package
// that is installed is not upgraded unless Latest is set. On Debian
// derivatives, a name with a version, such as "curl=8.5.0-1", is passed on
// unless that version is installed. Any other name with a version, release or
// glob in it cannot be checked, so it is always passed on. It has no timeout
// by default; WithTimeout and the ResourceTimeout option still apply.
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

	// Latest upgrades installed packages that have a newer version
	// available, as well as installing missing ones. What is available is
	// what the package lists held when they were last updated, such as by
	// AptUpdate. Only supported on Debian derivatives.
	Latest bool `json:",omitempty"`
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

	if p.Latest {
		if p.Hold || p.Unhold || p.Uninstall || p.Purge {
			return fmt.Errorf("cannot set Latest when not installing packages")
		}

		if !isDebian(viaduct.Attributes().Platform.ID) {
			return fmt.Errorf("upgrading to the latest version is not supported on %s", viaduct.Attributes().Platform.ID)
		}
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
	return isDebian(platform)
}

// isDebian reports whether the platform is a Debian derivative, using dpkg
// and apt.
func isDebian(platform string) bool {
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
	platform := viaduct.Attributes().Platform.ID

	if _, err := installArgs(platform, p.Names); err != nil {
		return err
	}

	states, err := packageStates(ctx, log, platform, p.Names)
	if err != nil {
		return err
	}

	missing := packagesToChange(p.Names, states, needsInstall)

	if isDebian(platform) {
		if missing, err = p.debianInstalls(ctx, log, missing, states); err != nil {
			return err
		}
	}

	if len(missing) == 0 {
		log.Noop("up-to-date", "packages", strings.Join(p.Names, ", "))
		return nil
	}

	if !change(ctx, log, "installing", "packages", strings.Join(missing, ", ")) {
		return nil
	}

	args, err := installArgs(platform, missing)
	if err != nil {
		return err
	}

	return runPkgCmd(ctx, log, args, p.Verbose)
}

func (p *Package) uninstall(ctx context.Context, log *viaduct.Logger) error {
	platform := viaduct.Attributes().Platform.ID

	if _, err := removeArgs(platform, p.Names, p.Purge); err != nil {
		return err
	}

	states, err := packageStates(ctx, log, platform, p.Names)
	if err != nil {
		return err
	}

	needed, msg := needsRemove, "uninstalling"
	if p.Purge {
		needed, msg = needsPurge, "purging"
	}

	present := packagesToChange(p.Names, states, needed)
	if len(present) == 0 {
		log.Noop("up-to-date", "packages", strings.Join(p.Names, ", "))
		return nil
	}

	if !change(ctx, log, msg, "packages", strings.Join(present, ", ")) {
		return nil
	}

	args, err := removeArgs(platform, present, p.Purge)
	if err != nil {
		return err
	}

	return runPkgCmd(ctx, log, args, p.Verbose)
}

// debianInstalls refines the packages to install on a Debian derivative:
// a pinned version that is already installed is dropped, and with Latest, an
// installed package with a newer candidate is added. The result keeps the
// order of Names.
func (p *Package) debianInstalls(ctx context.Context, log *viaduct.Logger, missing []string, states map[string]string) ([]string, error) {
	want := make(map[string]bool, len(missing))

	for _, name := range missing {
		pkg, version, ok := pinnedVersion(name)
		if !ok {
			want[name] = true
			continue
		}

		installed, err := installedVersion(ctx, log, pkg)
		if err != nil {
			return nil, err
		}

		want[name] = installed != version
	}

	if p.Latest {
		var installed []string
		for _, name := range p.Names {
			if queryable(name) && !needsInstall(states[name]) {
				installed = append(installed, name)
			}
		}

		upgrades, err := upgradable(ctx, log, installed)
		if err != nil {
			return nil, err
		}

		for name := range upgrades {
			want[name] = true
		}
	}

	var out []string
	for _, name := range p.Names {
		if want[name] {
			out = append(out, name)
		}
	}

	return out, nil
}

// pinnedVersion splits a Debian "name=version" into the package and its
// version, reporting whether name is one whose installed version can be
// looked up.
func pinnedVersion(name string) (pkg, version string, ok bool) {
	pkg, version, ok = strings.Cut(name, "=")

	return pkg, version, ok && version != "" && queryable(pkg) && queryable(version)
}

// installedVersion returns the installed version of a package, or "" if it
// is not installed. It runs in a dry run too.
func installedVersion(ctx context.Context, log *viaduct.Logger, pkg string) (string, error) {
	out, stderr, code, err := pkgQuery(ctx, log, "dpkg-query", "-W", "-f", "${db:Status-Status}\t${Version}\n", pkg)
	if err != nil {
		return "", err
	}

	if code != 0 && code != 1 {
		return "", queryError("dpkg-query", code, stderr)
	}

	for line := range strings.Lines(out) {
		state, version, ok := strings.Cut(strings.TrimRight(line, "\n"), "\t")
		if ok && state == stateInstalled {
			return version, nil
		}
	}

	return "", nil
}

// upgradable returns the packages whose candidate version, as apt-cache
// policy reports it, differs from the installed one. It runs in a dry run
// too.
func upgradable(ctx context.Context, log *viaduct.Logger, names []string) (map[string]bool, error) {
	if len(names) == 0 {
		return nil, nil
	}

	out, stderr, code, err := pkgQuery(ctx, log, "apt-cache", append([]string{"policy"}, names...)...)
	if err != nil {
		return nil, err
	}

	if code != 0 {
		return nil, queryError("apt-cache policy", code, stderr)
	}

	upgrades := make(map[string]bool)

	var name, installed string
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\n")

		// Each package starts with its name, unindented, followed by a colon
		if !strings.HasPrefix(line, " ") {
			name, installed = strings.TrimSuffix(line, ":"), ""
			continue
		}

		key, value, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if !ok {
			continue
		}

		switch key {
		case "Installed":
			installed = value
		case "Candidate":
			if installed != "" && installed != "(none)" && value != "(none)" && value != installed {
				upgrades[name] = true
			}
		}
	}

	return upgrades, nil
}

// Package states, named after dpkg's. rpm and pacman only ever report
// stateInstalled, or nothing for a package they do not have.
const (
	stateInstalled    = "installed"
	stateConfigFiles  = "config-files"
	stateNotInstalled = "not-installed"
)

// needsInstall reports whether a package in this state still needs
// installing. dpkg's triggers states come after configuration, so the
// package is usable; the others are a broken install that apt-get install
// finishes.
func needsInstall(state string) bool {
	switch state {
	case stateInstalled, "triggers-pending", "triggers-awaited":
		return false
	default:
		return true
	}
}

// needsRemove reports whether a package in this state still needs removing.
// A half-installed package counts, since it has files on disk.
func needsRemove(state string) bool {
	return state != "" && state != stateNotInstalled && state != stateConfigFiles
}

// needsPurge is needsRemove, plus a removed package whose configuration is
// still there
func needsPurge(state string) bool {
	return state != "" && state != stateNotInstalled
}

// packagesToChange returns the names whose state needs changing. A name that
// cannot be queried is always included, leaving the package manager to decide.
func packagesToChange(names []string, states map[string]string, needed func(string) bool) []string {
	var out []string

	for _, name := range names {
		if !queryable(name) || needed(states[name]) {
			out = append(out, name)
		}
	}

	return out
}

// queryable reports whether a name is a plain package name the query
// commands can look up. A version ("curl=8.5.0"), a release ("curl/sid") or a
// glob is something only the installing command understands: the query
// would report it as unknown, which would make removing it a noop.
func queryable(name string) bool {
	return !strings.ContainsAny(name, "=<>/*?[]")
}

// packageStates returns the state of each named package the package manager
// knows. A package missing from the map is not installed. It runs in a dry
// run too.
//
// Each query runs with LC_ALL=C because the "not installed" messages are
// parsed and would otherwise be translated, and with stderr captured so a
// noop does not log a line for every missing package.
func packageStates(ctx context.Context, log *viaduct.Logger, platform string, names []string) (map[string]string, error) {
	var query []string
	for _, name := range names {
		if queryable(name) {
			query = append(query, name)
		}
	}

	if len(query) == 0 {
		return map[string]string{}, nil
	}

	switch platform {
	case "debian", "ubuntu", "linuxmint":
		return dpkgStates(ctx, log, query)
	case "fedora", "centos":
		return rpmStates(ctx, log, query)
	case "arch", "manjaro":
		return pacmanStates(ctx, log, query)
	default:
		return nil, fmt.Errorf("unrecognised distribution: %s", platform)
	}
}

// pkgQuery runs a package query, returning its stdout and stderr, and its
// exit status. An error is returned only when the command could not run.
func pkgQuery(ctx context.Context, log *viaduct.Logger, name string, args ...string) (string, string, int, error) {
	var stderr bytes.Buffer

	out, err := viaduct.Command{
		Name:   name,
		Args:   args,
		Env:    []string{"LC_ALL=C"},
		Stderr: &stderr,
	}.Output(ctx, log)

	code, exited := viaduct.ExitCode(err)
	if err != nil && !exited {
		return "", "", 0, fmt.Errorf("%s failed: %w", name, err)
	}

	return string(out), stderr.String(), code, nil
}

// queryError describes a query that failed in a way its output does not
// explain
func queryError(name string, code int, stderr string) error {
	if msg := strings.TrimSpace(stderr); msg != "" {
		return fmt.Errorf("%s failed with exit status %d: %s", name, code, msg)
	}

	return fmt.Errorf("%s failed with exit status %d", name, code)
}

// dpkgStates asks dpkg for each package's ${db:Status-Status}, a single word
// such as "installed" or "config-files". The abbreviated "ii" form is not
// used because its first letter is the wanted state, which is "h" for a held
// package. Each line also carries both ${Package} and ${binary:Package}, so
// "libc6" and "libc6:arm64" are both found.
//
// dpkg-query exits 1 when any name is unknown to it, and still prints the
// rest, so that is not a failure. A purged package it still knows of is
// reported as "not-installed" with exit status 0.
func dpkgStates(ctx context.Context, log *viaduct.Logger, names []string) (map[string]string, error) {
	args := append([]string{"-W", "-f", "${Package}\t${binary:Package}\t${db:Status-Status}\n"}, names...)

	out, stderr, code, err := pkgQuery(ctx, log, "dpkg-query", args...)
	if err != nil {
		return nil, err
	}

	if code != 0 && code != 1 {
		return nil, queryError("dpkg-query", code, stderr)
	}

	states := make(map[string]string)
	for line := range strings.Lines(out) {
		fields := strings.Split(strings.TrimRight(line, "\n"), "\t")
		if len(fields) != 3 {
			continue
		}

		for _, name := range fields[:2] {
			// Two architectures of a package share ${Package}, so one
			// installed architecture is not hidden by another that is not
			if states[name] != stateInstalled {
				states[name] = fields[2]
			}
		}
	}

	return states, nil
}

// rpmStates runs "rpm -q", which prints "package NAME is not installed" on
// stdout for each missing name, and exits with the number of them. Anything
// else on stdout is an installed package's full version, which is not the
// name asked for, so installed is taken to be every name not reported
// missing. An exit status that does not match the count is a failed query.
func rpmStates(ctx context.Context, log *viaduct.Logger, names []string) (map[string]string, error) {
	out, stderr, code, err := pkgQuery(ctx, log, "rpm", append([]string{"-q"}, names...)...)
	if err != nil {
		return nil, err
	}

	missing := make(map[string]bool)
	var lines int
	for line := range strings.Lines(out) {
		name, ok := strings.CutPrefix(strings.TrimSpace(line), "package ")
		if !ok {
			continue
		}

		if name, ok = strings.CutSuffix(name, " is not installed"); ok {
			missing[name] = true
			lines++
		}
	}

	if code != min(lines, 255) {
		return nil, queryError("rpm -q", code, stderr)
	}

	return installedExcept(names, missing), nil
}

// pacmanStates runs "pacman -Q", which prints "error: package 'NAME' was not
// found" on stderr for each missing name and exits 1 if there are any. Its
// stdout is not used, since a name no package has falls back to an installed
// package that provides it, which is printed under its own name.
func pacmanStates(ctx context.Context, log *viaduct.Logger, names []string) (map[string]string, error) {
	_, stderr, code, err := pkgQuery(ctx, log, "pacman", append([]string{"-Q"}, names...)...)
	if err != nil {
		return nil, err
	}

	missing := make(map[string]bool)
	for line := range strings.Lines(stderr) {
		name, ok := strings.CutPrefix(strings.TrimSpace(line), "error: package '")
		if !ok {
			continue
		}

		if name, ok = strings.CutSuffix(name, "' was not found"); ok {
			missing[name] = true
		}
	}

	if (code == 0) != (len(missing) == 0) || code > 1 {
		return nil, queryError("pacman -Q", code, stderr)
	}

	return installedExcept(names, missing), nil
}

// installedExcept marks every name installed apart from the missing ones
func installedExcept(names []string, missing map[string]bool) map[string]string {
	states := make(map[string]string)

	for _, name := range names {
		if !missing[name] {
			states[name] = stateInstalled
		}
	}

	return states
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
	c := viaduct.Command{Name: args[0], Args: args[1:], Env: pkgEnv(args[0])}

	if !verbose {
		c.Stdout = io.Discard
	}

	return c.Run(ctx, log)
}

// dpkgNonInteractive answers the questions dpkg would otherwise ask on the
// terminal about a configuration file the package changes: a file that was
// edited locally is kept, and any other takes the package's version
var dpkgNonInteractive = []string{"--force-confdef", "--force-confold"}

// aptDpkgOptions passes dpkgNonInteractive through apt-get
var aptDpkgOptions = []string{
	"-o", "Dpkg::Options::=" + dpkgNonInteractive[0],
	"-o", "Dpkg::Options::=" + dpkgNonInteractive[1],
}

// pkgEnv stops debconf asking questions while apt-get or dpkg installs a
// package, since there is nobody to answer them
func pkgEnv(name string) []string {
	switch name {
	case "apt-get", "dpkg":
		return []string{"DEBIAN_FRONTEND=noninteractive"}
	default:
		return nil
	}
}

func aptGetArgs(command string, packages []string) []string {
	args := append([]string{"apt-get", command, "-y"}, aptDpkgOptions...)

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
