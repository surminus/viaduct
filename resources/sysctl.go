package resources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/surminus/viaduct"
)

// Sysctl writes a sysctl configuration file to /etc/sysctl.d and applies
// only that file, with "sysctl -p", if any of its values are not currently
// set.
type Sysctl struct {
	// Name is the name of the configuration file. A ".conf" suffix is
	// added if not present.
	Name string

	// Values are the sysctl keys and their desired values. As with sysctl, a
	// "/" in a dotted key stands for a dot within one component, so
	// net.ipv4.conf.eth0/100.forwarding is the forwarding key of eth0.100.
	Values map[string]string

	// IgnoreMissing lists keys in Values that the kernel may lack, such as
	// those of an unloaded module. They are written with a "-" prefix, so a
	// missing key is not an error when applying, and are only compared when
	// the kernel has them. A key the kernel has, but whose value it refuses,
	// is still an error. Optional.
	IgnoreMissing []string `json:",omitempty"`

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
func (s *Sysctl) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
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

	for key := range s.Values {
		if !validKey(key) {
			return fmt.Errorf("invalid sysctl key: %s", key)
		}
	}

	for _, key := range s.IgnoreMissing {
		if _, ok := s.Values[key]; !ok {
			return fmt.Errorf("IgnoreMissing key %s is not in Values", key)
		}
	}

	if err := requireRoot(ctx, "sysctl"); err != nil {
		return err
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

func (s *Sysctl) Run(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path := env.Path(s.path)
	content := s.content()

	// Checked before anything is written, as with -e a missing key is
	// skipped silently. This also covers dry runs.
	for _, key := range s.keys() {
		if !s.ignoresMissing(key) && !viaduct.FileExists(env.Path(procSysPath(key))) {
			return fmt.Errorf("sysctl key %s does not exist: load the module that provides it before this resource runs, or list it in IgnoreMissing if it may be absent", key)
		}
	}

	changed := true
	if viaduct.FileExists(path) {
		if current, err := os.ReadFile(path); err == nil {
			changed = string(current) != content
		} else {
			return err
		}
	}

	if !changed {
		log.Noop("up-to-date", "path", s.path)
	} else if change(ctx, log, "created", "path", s.path) {
		// Minimal systems may not have /etc/sysctl.d
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}

		if err := viaduct.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	if s.unapplied(env) == "" {
		log.Noop("applied", "path", s.path)
		return nil
	}

	if !change(ctx, log, "applied", "path", s.path) {
		return nil
	}

	// Not "sysctl --system", which would reapply every file and undo changes
	// made since boot. procps ignores the "-" prefix with -p, so -e is what
	// lets a missing key through.
	args := []string{"sysctl", "-p", s.path}
	if len(s.IgnoreMissing) > 0 {
		args = []string{"sysctl", "-e", "-p", s.path}
	}

	if err := runCommand(ctx, log, args...); err != nil {
		return fmt.Errorf("sysctl -p %s failed: %w", s.path, err)
	}

	// sysctl can exit 0 without having set everything, such as a key the
	// kernel rejected under -e
	if key := s.unapplied(env); key != "" {
		return fmt.Errorf("sysctl -p %s did not set %s", s.path, key)
	}

	return nil
}

// keys returns the keys in sorted order.
func (s *Sysctl) keys() []string {
	keys := make([]string, 0, len(s.Values))
	for k := range s.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

// ignoresMissing reports whether a key may be missing from the kernel.
func (s *Sysctl) ignoresMissing(key string) bool {
	return slices.Contains(s.IgnoreMissing, key)
}

// procSysPath returns the /proc/sys path of a key.
func procSysPath(key string) string {
	return filepath.Join("/proc", "sys", keyPath(key))
}

// keyPath turns a key into a path relative to /proc/sys. As in sysctl, a key
// whose first separator is a dot uses dots to separate and a slash for a
// literal dot, so net.ipv4.conf.eth0/100.forwarding is
// net/ipv4/conf/eth0.100/forwarding. A key whose first separator is a slash
// is already a path.
func keyPath(key string) string {
	if i := strings.IndexAny(key, "./"); i < 0 || key[i] == '/' {
		return key
	}

	return strings.Map(func(r rune) rune {
		switch r {
		case '.':
			return '/'
		case '/':
			return '.'
		default:
			return r
		}
	}, key)
}

// validKey reports whether a key names a file under /proc/sys, with no
// empty, "." or ".." component.
func validKey(key string) bool {
	for _, part := range strings.Split(keyPath(key), "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}

	return true
}

func (s *Sysctl) content() string {
	var b strings.Builder
	for _, k := range s.keys() {
		prefix := ""
		if s.ignoresMissing(k) {
			prefix = "-"
		}

		fmt.Fprintf(&b, "%s%s = %s\n", prefix, k, s.Values[k])
	}

	return b.String()
}

// unapplied returns the first key whose runtime value in /proc/sys does not
// match, or "" if every one does. A missing key listed in IgnoreMissing
// counts as applied.
func (s *Sysctl) unapplied(env viaduct.Env) string {
	for _, key := range s.keys() {
		current, err := os.ReadFile(env.Path(procSysPath(key)))
		if err != nil {
			if os.IsNotExist(err) && s.ignoresMissing(key) {
				continue
			}

			return key
		}

		// Normalise whitespace, since /proc/sys uses tabs for
		// multi-value keys
		if strings.Join(strings.Fields(string(current)), " ") != strings.Join(strings.Fields(s.Values[key]), " ") {
			return key
		}
	}

	return ""
}
