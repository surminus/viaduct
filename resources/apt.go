package resources

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
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
	// Update will perform an apt update after adding the repository. If the
	// update fails, the repository file is put back as it was, so the next
	// run tries again. A resource that updates has no timeout by default.
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
		params := viaduct.NewResourceParamsWithLockKey(viaduct.PackageLock)
		params.Timeout = noTimeout

		return params
	}

	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (a *Apt) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
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

	if err := requireRoot(ctx, "apt"); err != nil {
		return err
	}

	// Set optional defaults here
	if a.Distribution == "" {
		a.Distribution = viaduct.Attributes().Platform.UbuntuCodename
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

func (a *Apt) Run(ctx context.Context, log *viaduct.Logger) error {
	if a.UpdateOnly {
		return a.updateApt(ctx, log)
	}

	if a.Delete {
		return a.deleteApt(ctx, log)
	}

	return a.createApt(ctx, log)
}

// updateApt runs "apt-get update". It requires root in a dry run too.
func (a *Apt) updateApt(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)

	if !env.IsRoot() {
		return fmt.Errorf("must be run as root")
	}

	if !change(ctx, log, "updating") {
		return nil
	}

	return viaduct.Command{Name: "apt-get", Args: []string{"update", "-y"}, Stdout: io.Discard}.Run(ctx, log)
}

// createApt adds a new apt repository
func (a *Apt) createApt(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)

	var content string
	var err error

	if a.Format == List {
		content, err = a.listContent(ctx, log)
	} else {
		content, err = a.sourceContent(ctx, log)
	}
	if err != nil {
		return err
	}

	path := env.Path(a.path)
	altpath := env.Path(a.altpath)

	before, err := takeSnapshot(path, altpath)
	if err != nil {
		return err
	}

	if before.has(path, content) {
		log.Noop("up-to-date", "name", a.Name)
		return nil
	}

	if change(ctx, log, "created", "name", a.Name) {
		// Remove the other format, so the repository is not listed twice
		if err := os.Remove(altpath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		if err := viaduct.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	return a.updateOrRestore(ctx, log, before)
}

// updateOrRestore runs "apt-get update" if Update is set. If it fails, the
// files are put back as they were, so the next run sees the change again and
// retries the update.
func (a *Apt) updateOrRestore(ctx context.Context, log *viaduct.Logger, before snapshot) error {
	if !a.Update {
		return nil
	}

	err := a.updateApt(ctx, log)
	if err == nil || viaduct.EnvFrom(ctx).DryRun {
		return err
	}

	if rerr := before.restore(); rerr != nil {
		return errors.Join(err, fmt.Errorf("restoring the previous apt sources: %w", rerr))
	}

	return err
}

// snapshot holds the contents of files before a change, so they can be put
// back. A nil entry is a file that did not exist.
type snapshot map[string][]byte

// takeSnapshot reads the files at paths.
func takeSnapshot(paths ...string) (snapshot, error) {
	s := snapshot{}

	for _, p := range paths {
		b, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			s[p] = nil
			continue
		}

		if err != nil {
			return nil, err
		}

		s[p] = b
	}

	return s, nil
}

// has reports whether a file had the given content.
func (s snapshot) has(path, content string) bool {
	b := s[path]

	return b != nil && string(b) == content
}

// restore puts every file back as it was.
func (s snapshot) restore() error {
	var errs []error

	for p, b := range s {
		if b == nil {
			if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}

			continue
		}

		if err := viaduct.WriteFileAtomic(p, b, 0o644); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (a *Apt) listContent(ctx context.Context, log *viaduct.Logger) (string, error) {
	content := []string{
		"deb",
	}

	if a.SigningKey != "" || a.SigningKeyURL != "" {
		if err := a.receiveSigningKey(ctx, log); err != nil {
			return "", err
		}

		if a.Parameters == nil {
			a.Parameters = make(map[string]string)
		}

		a.Parameters["signed-by"] = a.signingKeyPath()
	}

	if len(a.Parameters) > 0 {
		var params []string

		// Sorted for a stable file
		for _, k := range slices.Sorted(maps.Keys(a.Parameters)) {
			params = append(params, fmt.Sprintf("%s=%s", k, a.Parameters[k]))
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

func (a *Apt) sourceContent(ctx context.Context, log *viaduct.Logger) (string, error) {
	content := []string{
		"Types: deb",
	}

	if a.SigningKey != "" || a.SigningKeyURL != "" {
		if err := a.receiveSigningKey(ctx, log); err != nil {
			return "", err
		}

		if a.Parameters == nil {
			a.Parameters = make(map[string]string)
		}

		content = append(content, fmt.Sprintf("Signed-By: %s", a.signingKeyPath()))
	}

	if a.PublicPgpKey != "" {
		content = append(content, fmt.Sprintf("Signed-By: %s", formatPublicGpgKey(a.PublicPgpKey)))
	}

	if len(a.Parameters) > 0 {
		for _, k := range slices.Sorted(maps.Keys(a.Parameters)) {
			v := a.Parameters[k]
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
// as a literal argument. A dry run only reports the fetch.
func (a *Apt) receiveSigningKey(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	keyPath := env.Path(a.signingKeyPath())

	if viaduct.FileExists(keyPath) {
		log.Noop("signing-key-exists", "path", keyPath)
		return nil
	}

	if env.DryRun {
		log.Info("signing-key-fetched", "path", keyPath)
		return nil
	}

	if a.SigningKeyURL != "" {
		// -f so an HTTP error is an error: without it curl exits 0 and the 404
		// body goes through gpg --dearmor, which passes non-armoured input
		// straight through, and the error page is installed as the keyring. The
		// existence check above then treats it as valid on every later run
		args := []string{"-sSfL"}

		// A key fetched over https is not let down to http by a redirect
		if strings.HasPrefix(strings.ToLower(a.SigningKeyURL), "https://") {
			args = append(args, "--proto", "=https", "--proto-redir", "=https")
		}

		key, err := viaduct.Command{Name: "curl", Args: append(args, a.SigningKeyURL)}.Output(ctx, log)
		if err != nil {
			return fmt.Errorf("could not fetch signing key from %s: %w", a.SigningKeyURL, err)
		}

		dearmoured, err := viaduct.Command{Name: "gpg", Args: []string{"--dearmor"}, Stdin: bytes.NewReader(key)}.Output(ctx, log)
		if err != nil {
			return err
		}

		if err := viaduct.WriteFileAtomic(keyPath, dearmoured, 0o644); err != nil {
			return err
		}
	}

	if a.SigningKey != "" {
		// First we fetch the key using GPG
		if err := runCommand(ctx, log, "gpg", "--recv-keys", "--keyserver", "keyserver.ubuntu.com", a.SigningKey); err != nil {
			return err
		}

		// Ensure that the key is deleted from GPG
		defer func() {
			//nolint:errcheck
			runCommand(ctx, log, "gpg", "--delete-keys", "--yes", a.SigningKey)
		}()

		// Then we export the key to disk
		exported, err := viaduct.Command{Name: "gpg", Args: []string{"--export", a.SigningKey}}.Output(ctx, log)
		if err != nil {
			return err
		}

		if err := viaduct.WriteFileAtomic(keyPath, exported, 0o644); err != nil {
			return err
		}
	}

	log.Info("signing-key-fetched", "path", keyPath)
	return nil
}

// signingKeyPath is the keyring path, without the root applied
func (a *Apt) signingKeyPath() string {
	return filepath.Join("/usr/share/keyrings", fmt.Sprintf("%s.gpg", a.Name))
}

// deleteApt removes an apt repository
func (a *Apt) deleteApt(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path := env.Path(a.path)

	if !viaduct.FileExists(path) {
		log.Noop("up-to-date", "name", a.Name)
		return nil
	}

	before, err := takeSnapshot(path)
	if err != nil {
		return err
	}

	if change(ctx, log, "deleted", "name", a.Name) {
		if err := os.Remove(path); err != nil {
			return err
		}
	}

	return a.updateOrRestore(ctx, log, before)
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
