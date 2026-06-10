package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/surminus/viaduct"
)

// Sysctl writes a sysctl configuration file to /etc/sysctl.d and applies
// it with "sysctl --system" if any of the values are not currently set.
type Sysctl struct {
	// Name is the name of the configuration file. A ".conf" suffix is
	// added if not present.
	Name string

	// Values are the sysctl keys and their desired values
	Values map[string]string

	// path is a private attribute for where to write the file
	path string
}

func (s *Sysctl) Description() string {
	return s.Name
}

func (s *Sysctl) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (s *Sysctl) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if s.Name == "" {
		return fmt.Errorf("required parameter: Name")
	}

	// Name becomes a filename under /etc/sysctl.d, so reject anything
	// that could escape that directory.
	if s.Name != filepath.Base(s.Name) || s.Name == "." || s.Name == ".." {
		return fmt.Errorf("name must be a plain filename, not a path: %s", s.Name)
	}

	if len(s.Values) == 0 {
		return fmt.Errorf("required parameter: Values")
	}

	if !viaduct.IsRoot() {
		return fmt.Errorf("sysctl resource must be run as root")
	}

	// Set optional defaults here
	name := s.Name
	if !strings.HasSuffix(name, ".conf") {
		name = name + ".conf"
	}
	s.path = filepath.Join("/etc", "sysctl.d", name)

	return nil
}

func (s *Sysctl) OperationName() string {
	return "Apply"
}

func (s *Sysctl) Run(log *viaduct.Logger) error {
	if viaduct.Cli.DryRun {
		log.Info("applied", "path", s.path)
		return nil
	}

	content := s.content()

	changed := true
	if viaduct.FileExists(s.path) {
		if current, err := os.ReadFile(s.path); err == nil {
			changed = string(current) != content
		} else {
			return err
		}
	}

	if changed {
		// Minimal systems may not have /etc/sysctl.d
		if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
			return err
		}

		if err := os.WriteFile(s.path, []byte(content), 0o644); err != nil {
			return err
		}

		log.Info("created", "path", s.path)
	} else {
		log.Noop("up-to-date", "path", s.path)
	}

	if s.valuesApplied() {
		log.Noop("applied", "path", s.path)
		return nil
	}

	if err := runCommand("sysctl", "--system"); err != nil {
		return fmt.Errorf("sysctl --system failed: %w", err)
	}

	log.Info("applied", "path", s.path)

	return nil
}

func (s *Sysctl) content() string {
	keys := make([]string, 0, len(s.Values))
	for k := range s.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %s\n", k, s.Values[k])
	}

	return b.String()
}

// valuesApplied returns true if all values match the current runtime
// values in /proc/sys
func (s *Sysctl) valuesApplied() bool {
	for key, value := range s.Values {
		path := filepath.Join("/proc", "sys", strings.ReplaceAll(key, ".", "/"))

		current, err := os.ReadFile(path)
		if err != nil {
			return false
		}

		// Normalise whitespace, since /proc/sys uses tabs for
		// multi-value keys
		if strings.Join(strings.Fields(string(current)), " ") != strings.Join(strings.Fields(value), " ") {
			return false
		}
	}

	return true
}
