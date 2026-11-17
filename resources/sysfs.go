package resources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/surminus/viaduct"
)

// SysfsValue sets a value in /sys, such as
// /sys/kernel/mm/transparent_hugepage/enabled. For a file that lists choices
// with the current one in brackets, such as "always madvise [never]", the
// bracketed choice is compared. The value lasts until the next boot.
type SysfsValue struct {
	// Path is the file under /sys.
	Path string

	// Value is what to write to it.
	Value string
}

func (s *SysfsValue) Description() string {
	return s.Path
}

func (s *SysfsValue) OperationName() string {
	return "Set"
}

func (s *SysfsValue) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

func (s *SysfsValue) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	if s.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	if clean := filepath.Clean(s.Path); clean != s.Path || !strings.HasPrefix(clean, "/sys/") {
		return fmt.Errorf("path must be a clean absolute path under /sys: %s", s.Path)
	}

	if strings.TrimSpace(s.Value) == "" {
		return fmt.Errorf("required parameter: Value")
	}

	return requireRoot(ctx, "sysfs")
}

func (s *SysfsValue) Run(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path := env.Path(s.Path)
	want := strings.TrimSpace(s.Value)

	current, err := readSysfs(path)
	if err != nil {
		return err
	}

	if current == want {
		log.Noop("up-to-date", "path", s.Path, "value", want)
		return nil
	}

	if !change(ctx, log, "set", "path", s.Path, "value", want, "was", current) {
		return nil
	}

	if err := writeSysfs(path, want); err != nil {
		return fmt.Errorf("writing %s to %s: %w", want, s.Path, err)
	}

	// The kernel can accept a write but keep another value.
	if got, err := readSysfs(path); err != nil {
		return err
	} else if got != want {
		return fmt.Errorf("%s is %s after writing %s", s.Path, got, want)
	}

	return nil
}

// writeSysfs writes a value to a sysfs file in place with a single write, as
// sysfs files cannot be replaced. It is a variable so tests can make the
// kernel refuse or change a value.
var writeSysfs = func(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}

	if _, err := f.WriteString(value); err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

// readSysfs reads the current value of a sysfs file.
func readSysfs(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return sysfsSelected(string(b)), nil
}

// sysfsSelected returns the bracketed choice from the contents of a sysfs
// file, or the whole value if there are none.
func sysfsSelected(content string) string {
	content = strings.TrimSpace(content)

	if start := strings.IndexByte(content, '['); start >= 0 {
		if end := strings.IndexByte(content[start:], ']'); end > 0 {
			return content[start+1 : start+end]
		}
	}

	return content
}
