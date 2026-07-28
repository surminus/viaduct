package resources

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/surminus/viaduct"
)

type AptFormat string

const (
	List    AptFormat = "list"
	Sources AptFormat = "sources"
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
	// the Ubuntu codename. For Sources format, this represents Suites.
	Distribution string
	// Source is repository type. Defaults to main. For Sources format, this
	// represents Components.
	Source string
	// Parameters is a map of optional parameters that gets represented as key
	// value pairs, eg "[arch=amd64]"
	Parameters map[string]string
	// SigningKey will use the legacy apt-key command to retrieve a key
	SigningKey string
	// SigningKeyURL will retrieve the signing key for the package,
	// and include it as part of the source list
	SigningKeyURL string
	// Format will either use the list or sources format
	Format AptFormat
	// PublicPgpKey is just a string representation of a public key. This is
	// only applicable to Sources format.
	PublicPgpKey string

	// Delete will remove the apt repository if set to true.
	Delete bool
	// Update will perform an apt update after adding the repository.
	Update bool
	// UpdateOnly will only perform an apt update.
	UpdateOnly bool

	// path is a private attribute for where to write the apt file
	path string
	// altpath is the path of the alternative format, so we can ensure it
	// gets removed
	altpath string
}

func (a *Apt) Description() string {
	if a.UpdateOnly {
		return "apt-update"
	}
	return a.Name
}

// Params allows the resource to dynamically set options that will be passed
// at compile time
func (a *Apt) Params() *viaduct.ResourceParams {
	// Only the update takes the package lock; writing a sources file
	// contends with nothing
	if a.Update || a.UpdateOnly {
		return viaduct.NewResourceParamsWithLockKey(viaduct.PackageLock)
	}

	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (a *Apt) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if a.UpdateOnly {
		return nil
	}

	if a.Name == "" {
		return fmt.Errorf("required parameter: Name")
	}

	if a.URI == "" {
		return fmt.Errorf("required parameter: URI")
	}

	if !viaduct.IsRoot() {
		return fmt.Errorf("apt resource must be run as root")
	}

	// Set optional defaults here
	if a.Distribution == "" {
		a.Distribution = viaduct.Attribute.Platform.UbuntuCodename
	}

	if a.Source == "" {
		a.Source = "main"
	}

	if a.Format == "" {
		a.Format = List
	}

	if a.PublicPgpKey != "" && a.Format == List {
		return fmt.Errorf("cannot set PublicPgpKey with list format")
	}

	if a.SigningKey != "" && a.SigningKeyURL != "" {
		return fmt.Errorf("cannot set both SigningKey and SigningKeyURL")
	}

	if a.PublicPgpKey != "" && (a.SigningKey != "" || a.SigningKeyURL != "") {
		return fmt.Errorf("cannot set both PublicPgpKey and SigningKey/SigningKeyURL")
	}

	rootpath := filepath.Join("/etc", "apt", "sources.list.d")
	if a.Format == List {
		a.path = filepath.Join(rootpath, fmt.Sprintf("%s.list", a.Name))
		a.altpath = filepath.Join(rootpath, fmt.Sprintf("%s.sources", a.Name))
	} else {
		a.path = filepath.Join(rootpath, fmt.Sprintf("%s.sources", a.Name))
		a.altpath = filepath.Join(rootpath, fmt.Sprintf("%s.list", a.Name))
	}

	return nil
}

func AptUpdate() *Apt {
	return &Apt{UpdateOnly: true}
}

// DistUpgrade performs an apt-get dist-upgrade
func DistUpgrade() *Execute {
	return aptExec("apt-get", "dist-upgrade", "-q", "-y")
}

// AptAutoremove removes packages that are no longer required
func AptAutoremove() *Execute {
	return aptExec("apt-get", "autoremove", "-q", "-y")
}

// AptHold marks packages as held back from upgrades. The Hold option on the
// Package resource does the same thing and skips packages that are already
// held, so prefer that unless you specifically want the command to run.
func AptHold(names ...string) *Execute {
	return aptExec(append([]string{"apt-mark", "hold"}, names...)...)
}

// InstallDeb installs a deb package from a file using dpkg
func InstallDeb(path string) *Execute {
	return aptExec("dpkg", "-i", viaduct.ExpandPath(path))
}

// aptExec builds a command that runs without a shell, so package names and
// paths containing spaces or shell metacharacters are passed through
// unmangled. It takes the package lock rather than the global one, so it
// serialises against other package work only.
func aptExec(args ...string) *Execute {
	return &Execute{Args: args, LockKey: viaduct.PackageLock}
}

func (a *Apt) OperationName() string {
	if a.Delete {
		return "Delete"
	}

	if a.UpdateOnly {
		return "Update"
	}

	return "Create"
}

func (a *Apt) Run(log *viaduct.Logger) error {
	if a.UpdateOnly {
		return a.updateApt(log)
	}

	if a.Delete {
		return a.deleteApt(log)
	} else {
		return a.createApt(log)
	}
}

// AptUpdate is a helper function to perform "apt-get update"
// Should be converted to a proper resource
func (a *Apt) updateApt(log *viaduct.Logger) error {
	if viaduct.Cli.DryRun {
		log.Info("updating")
		return nil
	}

	if !viaduct.IsRoot() {
		return fmt.Errorf("must be run as root")
	}

	log.Info("updating")

	cmd := exec.Command("apt-get", "update", "-y")
	cmd.Stderr = aptStderr()

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func aptStderr() *os.File {
	if viaduct.Cli.JSON {
		return nil
	}
	return os.Stderr
}

// Create adds a new apt repository
func (a *Apt) createApt(log *viaduct.Logger) error {
	if viaduct.Cli.DryRun {
		log.Info("created", "name", a.Name)
		return nil
	}

	var content string
	var err error

	if a.Format == List {
		content, err = a.listContent(log)
	} else {
		content, err = a.sourceContent(log)
	}
	if err != nil {
		return err
	}

	if viaduct.FileExists(a.path) {
		if con, err := os.ReadFile(a.path); err == nil {
			if string(con) == content {
				log.Noop("up-to-date", "name", a.Name)
				return nil
			}
		} else {
			return err
		}
	}

	// Remove the other type so we don't have repeats
	if viaduct.FileExists(a.altpath) {
		if err := os.Remove(a.altpath); err != nil {
			return err
		}
	}

	if err := os.WriteFile(a.path, []byte(content), 0o644); err != nil {
		return err
	}

	log.Info("created", "name", a.Name)

	if a.Update {
		return a.updateApt(log)
	}

	return nil
}

func (a *Apt) listContent(log *viaduct.Logger) (string, error) {
	content := []string{
		"deb",
	}

	if a.SigningKey != "" || a.SigningKeyURL != "" {
		if err := a.receiveSigningKey(log); err == nil {
			if a.Parameters == nil {
				a.Parameters = make(map[string]string)
			}

			a.Parameters["signed-by"] = a.signingKeyPath()
		} else {
			return "", err
		}
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

	return strings.Join(content, " "), nil
}

func (a *Apt) sourceContent(log *viaduct.Logger) (string, error) {
	content := []string{
		"Types: deb",
	}

	if a.SigningKey != "" || a.SigningKeyURL != "" {
		if err := a.receiveSigningKey(log); err == nil {
			if a.Parameters == nil {
				a.Parameters = make(map[string]string)
			}

			content = append(content, fmt.Sprintf("Signed-By: %s", a.signingKeyPath()))
		} else {
			return "", err
		}
	}

	if a.PublicPgpKey != "" {
		content = append(content, fmt.Sprintf("Signed-By: %s", formatPublicGpgKey(a.PublicPgpKey)))
	}

	if len(a.Parameters) > 0 {
		for k, v := range a.Parameters {
			if k == "arch" {
				content = append(content, fmt.Sprintf("Architectures: %s", v))
				continue
			}

			content = append(content, fmt.Sprintf("%s: %s", k, v))
		}
	}

	content = append(content, []string{
		fmt.Sprintf("URIs: %s", a.URI),
		fmt.Sprintf("Suites: %s", a.Distribution),
		fmt.Sprintf("Components: %s", a.Source),
		"\n",
	}...)

	return strings.Join(content, "\n"), nil
}

// receiveSigningKey will fetch a signing key. The commands run without a
// shell, so a URL or key ID containing shell metacharacters is passed through
// as a literal argument rather than being interpreted.
func (a *Apt) receiveSigningKey(log *viaduct.Logger) error {
	if viaduct.FileExists(a.signingKeyPath()) {
		log.Noop("signing-key-exists", "path", a.signingKeyPath())
		return nil
	}

	if a.SigningKeyURL != "" {
		// -f so an HTTP error is an error: without it curl exits 0 and the 404
		// body goes through gpg --dearmor, which passes non-armoured input
		// straight through, and the error page is installed as the keyring. The
		// existence check above then treats it as valid on every later run
		// nolint:gosec
		cmd := exec.Command("curl", "-sSfL", a.SigningKeyURL)
		cmd.Stderr = aptStderr()

		key, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("could not fetch signing key from %s: %w", a.SigningKeyURL, err)
		}

		if err := writeCommandOutput(a.signingKeyPath(), bytes.NewReader(key), "gpg", "--dearmor"); err != nil {
			return err
		}
	}

	if a.SigningKey != "" {
		// First we fetch the key using GPG
		if err := runCommand("gpg", "--recv-keys", "--keyserver", "keyserver.ubuntu.com", a.SigningKey); err != nil {
			return err
		}

		// Ensure that the key is deleted from GPG
		defer func() {
			//nolint:errcheck
			runCommand("gpg", "--delete-keys", "--yes", a.SigningKey)
		}()

		// Then we export the key to disk
		if err := writeCommandOutput(a.signingKeyPath(), nil, "gpg", "--export", a.SigningKey); err != nil {
			return err
		}
	}

	log.Info("signing-key-fetched", "path", a.signingKeyPath())
	return nil
}

// writeCommandOutput runs a command and writes its standard output to path.
// The content goes to a temporary file that is moved into place, so a command
// that fails part way through does not leave a truncated file behind for the
// next run to treat as valid.
func writeCommandOutput(path string, stdin io.Reader, args ...string) error {
	tmp := path + ".viaduct-tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	// nolint:gosec
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = stdin
	cmd.Stdout = f
	cmd.Stderr = aptStderr()

	if runErr := cmd.Run(); runErr != nil {
		f.Close()
		os.Remove(tmp)

		return runErr
	}

	if err := f.Close(); err != nil {
		os.Remove(tmp)

		return err
	}

	return os.Rename(tmp, path)
}

func (a *Apt) signingKeyPath() string {
	return filepath.Join("/usr/share/keyrings", fmt.Sprintf("%s.gpg", a.Name))
}

// Delete removes an apt repository
func (a *Apt) deleteApt(log *viaduct.Logger) error {
	if viaduct.Cli.DryRun {
		log.Info("deleted", "name", a.Name)
		return nil
	}

	if !viaduct.FileExists(a.path) {
		log.Noop("up-to-date", "name", a.Name)
		return nil
	}

	if err := os.Remove(a.path); err != nil {
		return err
	}

	log.Info("deleted", "name", a.Name)

	if a.Update {
		return a.updateApt(log)
	}

	return nil
}

// formatPublicGpgKey by adding an indent and adding a dot to any empty line
func formatPublicGpgKey(original string) string {
	var result []string
	splitkey := strings.Split(original, "\n")
	for i, line := range splitkey {
		// ignore an empty last line
		if i == len(splitkey)-1 && line == "" {
			break
		}

		if line == "" {
			line = "."
		}

		line = " " + line
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}
