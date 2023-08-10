package resources

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/surminus/viaduct"
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
	// the Ubuntu codename.
	Distribution string
	// Source is repository type. Defaults to main.
	Source string
	// Parameters is a map of optional parameters that gets represented as key
	// value pairs, eg "[arch=amd64]"
	Parameters map[string]string
	// SigningKey will use the legacy apt-key command to retrieve a key
	SigningKey string
	// SigningKeyURL will retrieve the signing key for the package,
	// and include it as part of the source list
	SigningKeyURL string

	// Delete will remove the apt repository if set to true.
	Delete bool
	// Update will perform an apt update after adding the repository.
	Update bool
	// UpdateOnly will only perform an apt update.
	UpdateOnly bool

	// path is a private attribute for where to write the apt file
	path string
}

// Params allows the resource to dynamically set options that will be passed
// at compile time
func (a *Apt) Params() *viaduct.ResourceParams {
	return &viaduct.ResourceParams{GlobalLock: a.Update || a.UpdateOnly}
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (a *Apt) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if a.UpdateOnly {
		return nil
	}

	if a.Name == "" {
		return fmt.Errorf("Required parameter: Name")
	}

	if a.URI == "" {
		return fmt.Errorf("Required parameter: URI")
	}

	if !viaduct.IsRoot() {
		return fmt.Errorf("Apt resource must be run as root")
	}

	// Set optional defaults here
	if a.Distribution == "" {
		a.Distribution = viaduct.Attribute.Platform.UbuntuCodename
	}

	if a.Source == "" {
		a.Source = "main"
	}

	if a.SigningKey != "" && a.SigningKeyURL != "" {
		return fmt.Errorf("Cannot set both SigningKey and SigningKeyURL")
	}

	a.path = filepath.Join("/etc", "apt", "sources.list.d", fmt.Sprintf("%s.list", a.Name))

	return nil
}

func AptUpdate() *Apt {
	return &Apt{UpdateOnly: true}
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
	if viaduct.Config.DryRun {
		log.Info()
		return nil
	}

	if !viaduct.IsRoot() {
		return fmt.Errorf("must be run as root")
	}

	log.Info()

	command := []string{"apt-get", "update", "-y"}

	cmd := exec.Command("bash", "-c", strings.Join(command, " "))
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

// Create adds a new apt repository
func (a *Apt) createApt(log *viaduct.Logger) error {
	if viaduct.Config.DryRun {
		log.Info(a.Name)
		return nil
	}

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
			return err
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

	if viaduct.FileExists(a.path) {
		if con, err := os.ReadFile(a.path); err == nil {
			if string(con) == strings.Join(content, " ") {
				log.Noop(a.Name)
				return nil
			}
		} else {
			return err
		}
	}

	if err := os.WriteFile(a.path, []byte(strings.Join(content, " ")), 0o644); err != nil {
		return err
	}

	log.Info(a.Name)

	if a.Update {
		return a.updateApt(log)
	}

	return nil
}

// receiveSigningKey will fetch a signing key
func (a *Apt) receiveSigningKey(log *viaduct.Logger) error {
	if viaduct.FileExists(a.signingKeyPath()) {
		log.Noop("Signing key: ", a.signingKeyPath())
		return nil
	}

	if a.SigningKeyURL != "" {
		command := []string{"curl", "-sS", a.SigningKeyURL, "|", "gpg", "--dearmor", "|", "tee", a.signingKeyPath()}

		cmd := exec.Command("bash", "-c", strings.Join(command, " "))
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return nil
		}
	}

	if a.SigningKey != "" {
		// First we fetch the key using GPG
		command := []string{"gpg", "--recv-keys", "--keyserver", "keyserver.ubuntu.com", a.SigningKey}

		cmd := exec.Command("bash", "-c", strings.Join(command, " "))
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return nil
		}

		// Then we export the key to disk
		command = []string{"gpg", "--export", a.SigningKey, "|", "tee", a.signingKeyPath()}

		cmd = exec.Command("bash", "-c", strings.Join(command, " "))
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return nil
		}

		// Ensure that the key is deleted from GPG
		defer func() {
			command = []string{"gpg", "--delete-keys", "--yes", a.SigningKey}
			cmd = exec.Command("bash", "-c", strings.Join(command, " "))
			cmd.Env = []string{"DEBIAN_FRONTEND=noninteractive"}
			cmd.Run()
		}()
	}

	log.Info("Signing key: ", a.signingKeyPath())
	return nil
}

func (a *Apt) signingKeyPath() string {
	return filepath.Join("/usr/share/keyrings", fmt.Sprintf("%s.gpg", a.Name))
}

// Delete removes an apt repository
func (a *Apt) deleteApt(log *viaduct.Logger) error {
	if viaduct.Config.DryRun {
		log.Info(a.Name)
		return nil
	}

	if !viaduct.FileExists(a.path) {
		log.Noop(a.Name)
		return nil
	}

	if err := os.Remove(a.path); err != nil {
		return err
	}

	log.Info(a.Name)

	if a.Update {
		return a.updateApt(log)
	}

	return nil
}
