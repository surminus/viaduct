package resources

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/surminus/viaduct"
)

// KernelModule loads a kernel module with modprobe, unless it is already
// loaded or built into the kernel.
type KernelModule struct {
	// Name is the module, such as br_netfilter.
	Name string

	// Persist also loads the module at boot, by writing its name to
	// /etc/modules-load.d/<Name>.conf. Optional.
	Persist bool
}

// LoadModule is a shortcut for loading a kernel module now and at boot.
func LoadModule(name string) *KernelModule {
	return &KernelModule{Name: name, Persist: true}
}

// moduleNamePattern restricts a module name to what modprobe accepts and is
// safe in a file name.
var moduleNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (k *KernelModule) Description() string {
	return k.Name
}

func (k *KernelModule) OperationName() string {
	return "Load"
}

func (k *KernelModule) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

func (k *KernelModule) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	if k.Name == "" {
		return fmt.Errorf("required parameter: Name")
	}

	if !moduleNamePattern.MatchString(k.Name) {
		return fmt.Errorf("invalid module name: %s", k.Name)
	}

	return requireRoot(ctx, "kernel module")
}

func (k *KernelModule) Run(ctx context.Context, log *viaduct.Logger) error {
	if k.Persist {
		if err := k.persist(ctx, log); err != nil {
			return err
		}
	}

	env := viaduct.EnvFrom(ctx)
	name := moduleName(k.Name)

	// The kernel lists a loaded module under /sys/module. A built-in module
	// is listed there only when it has parameters, so modules.builtin is
	// checked too
	if viaduct.DirExists(env.Path("/sys/module/" + name)) {
		log.Noop("loaded", "module", k.Name)
		return nil
	}

	if builtinModule(env, name) {
		log.Noop("built-in", "module", k.Name)
		return nil
	}

	if !change(ctx, log, "loaded", "module", k.Name) {
		return nil
	}

	if err := runCommand(ctx, log, "modprobe", k.Name); err != nil {
		return fmt.Errorf("modprobe %s failed: %w", k.Name, err)
	}

	return nil
}

// persist writes the modules-load.d file that loads the module at boot.
// moduleName is a module's name as the kernel lists it, with dashes as
// underscores, whichever was used to load it
func moduleName(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// builtinModule reports whether the running kernel has the module built in,
// from the modules.builtin list for its release. A list that cannot be read
// counts as the module not being built in, so modprobe decides.
func builtinModule(env viaduct.Env, name string) bool {
	release, err := os.ReadFile(env.Path("/proc/sys/kernel/osrelease"))
	if err != nil {
		return false
	}

	list, err := os.ReadFile(env.Path(filepath.Join("/lib/modules", strings.TrimSpace(string(release)), "modules.builtin")))
	if err != nil {
		return false
	}

	// Each line is a path such as kernel/drivers/block/loop.ko
	for _, line := range strings.Split(string(list), "\n") {
		if moduleName(strings.TrimSuffix(path.Base(strings.TrimSpace(line)), ".ko")) == name {
			return true
		}
	}

	return false
}

func (k *KernelModule) persist(ctx context.Context, log *viaduct.Logger) error {
	conf := "/etc/modules-load.d/" + k.Name + ".conf"
	path, err := viaduct.EnvFrom(ctx).ResolveSymlink(conf)
	if err != nil {
		return err
	}
	content := k.Name + "\n"

	if current, err := os.ReadFile(path); err == nil && string(current) == content {
		log.Noop("up-to-date", "path", conf)
		return nil
	}

	if !change(ctx, log, "created", "path", conf) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return viaduct.WriteFileAtomic(path, []byte(content), 0o644)
}
