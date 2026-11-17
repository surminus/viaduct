package resources

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/surminus/viaduct"
)

// Hostname sets the host name with "hostnamectl set-hostname", changing
// both the running name and /etc/hostname. It does not edit /etc/hosts.
type Hostname struct {
	// Name is the host name.
	Name string

	// Transient sets only the running name, with "hostname", leaving
	// /etc/hostname alone. The name lasts until the next boot.
	Transient bool
}

// SetHostname returns a Hostname that sets the host name.
func SetHostname(name string) *Hostname {
	return &Hostname{Name: name}
}

// hostnamePattern matches one label of a host name.
var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func (h *Hostname) Description() string {
	return h.Name
}

func (h *Hostname) OperationName() string {
	return "Set"
}

func (h *Hostname) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParamsWithLockKey(viaduct.NetworkLock)
}

func (h *Hostname) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	if h.Name == "" {
		return fmt.Errorf("required parameter: Name")
	}

	// The kernel holds at most 64 characters.
	if len(h.Name) > 64 {
		return fmt.Errorf("host name is longer than 64 characters: %s", h.Name)
	}

	for _, label := range strings.Split(h.Name, ".") {
		if !hostnamePattern.MatchString(label) {
			return fmt.Errorf("invalid host name: %s", h.Name)
		}
	}

	return requireRoot(ctx, "hostname")
}

func (h *Hostname) Run(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)

	// The file os.Hostname reads, read directly so the root applies.
	current, err := readTrimmed(env.Path("/proc/sys/kernel/hostname"))
	if err != nil {
		return err
	}

	upToDate := current == h.Name
	command := []string{"hostnamectl", "set-hostname", h.Name}

	if h.Transient {
		command = []string{"hostname", h.Name}
	} else {
		static, err := readTrimmed(env.Path("/etc/hostname"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		upToDate = upToDate && static == h.Name
	}

	if upToDate {
		log.Noop("up-to-date", "name", h.Name)
		return nil
	}

	if !change(ctx, log, "set", "name", h.Name, "was", current) {
		return nil
	}

	return runCommand(ctx, log, command...)
}

// readTrimmed reads a file without its surrounding whitespace.
func readTrimmed(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(b)), nil
}
