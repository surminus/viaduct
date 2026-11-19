package resources

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/surminus/viaduct"
)

// Mount mounts a filesystem and records it in /etc/fstab so it is mounted at
// boot. It creates the mount point, mounts the filesystem, and only once that
// succeeds keeps a single fstab entry for the path. A path already mounted
// from somewhere else is an error, and so is a mount point that is a symlink.
type Mount struct {
	// Source is what to mount: a device such as /dev/sdb, a tag such as
	// UUID=..., tmpfs, or a directory when Options include bind or rbind.
	Source string

	// Path is the mount point.
	Path string

	// FSType is the filesystem type, such as ext4.
	FSType string

	// Options are the mount options. The default is "defaults".
	Options string

	// Dump is the fifth fstab field, whether dump backs the filesystem up.
	// The default is 0.
	Dump int

	// Pass is the sixth fstab field, the order fsck checks the filesystem
	// in at boot. The default is 0, not checked.
	Pass int

	// Mode is the mode of the mount point, if created. The default is 0755.
	Mode os.FileMode
}

func (m *Mount) Description() string {
	return m.Path
}

func (m *Mount) OperationName() string {
	return "Mount"
}

func (m *Mount) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParamsWithLockKey(viaduct.FstabLock)
}

func (m *Mount) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	if m.Source == "" {
		return fmt.Errorf("required parameter: Source")
	}

	if m.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	if !filepath.IsAbs(m.Path) || filepath.Clean(m.Path) == "/" {
		return fmt.Errorf("path must be an absolute path other than /: %s", m.Path)
	}

	if m.FSType == "" {
		return fmt.Errorf("required parameter: FSType")
	}

	if m.Options == "" {
		m.Options = "defaults"
	}

	if m.Mode == 0 {
		m.Mode = 0o755
	}

	return requireRoot(ctx, "mount")
}

func (m *Mount) Run(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path := filepath.Clean(m.Path)

	// mount follows a symlink at the mount point, which would mount over
	// wherever it points
	if info, err := os.Lstat(env.Path(path)); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return viaduct.Permanent(fmt.Errorf("mount point %s is a symlink", path))
	}

	// Fail before changing anything if something else is mounted there.
	current, mounted, err := mountedAt(env, resolvePath(env, path))
	if err != nil {
		return err
	}

	if mounted && !m.isMounted(env, current) {
		from := current.Source
		if current.Root != "/" {
			from += "[" + current.Root + "]"
		}

		return viaduct.Permanent(fmt.Errorf("%s is already mounted from %s, not %s; unmount it or change Source", path, from, m.Source))
	}

	if err := m.ensureDirectory(ctx, log, path); err != nil {
		return err
	}

	// Mount before recording it, so a mount that fails never leaves an
	// fstab entry that could stop the host booting.
	if mounted {
		log.Noop("mounted", "path", path, "source", current.Source)
	} else if change(ctx, log, "mounted", "path", path, "source", m.Source) {
		if err := runCommand(ctx, log, "mount", "-t", m.FSType, "-o", m.Options, m.Source, path); err != nil {
			return fmt.Errorf("mounting %s: %w", path, err)
		}
	}

	changed, err := ensureFstabEntry(ctx, log, fstabEntry{
		Source:  m.Source,
		Path:    path,
		FSType:  m.FSType,
		Options: m.Options,
		Dump:    m.Dump,
		Pass:    m.Pass,
	}, func(fields []string) bool {
		return len(fields) > 1 && filepath.Clean(fields[1]) == path
	})
	if err != nil {
		return err
	}

	if changed {
		return reloadSystemd(ctx, log)
	}

	return nil
}

// isMounted reports whether the mount at the path is this one.
//
// A bind mount's mountinfo source is the device underneath, not the
// directory, so it is compared by where it comes from instead: the same
// device, and the same directory within that device's filesystem as Source
// resolves to. Anything else compares its source as a device.
func (m *Mount) isMounted(env viaduct.Env, current mountInfo) bool {
	if !m.isBind() {
		return sameDevice(env, current.Source, m.Source) && m.sameRoot(current.Root)
	}

	all, err := mounts(env)
	if err != nil {
		return false
	}

	source := resolvePath(env, m.Source)

	// The mount holding the source is the deepest one containing it.
	var holder mountInfo

	for _, mi := range all {
		if pathWithin(source, mi.Path) && len(mi.Path) >= len(holder.Path) {
			holder = mi
		}
	}

	rel, err := filepath.Rel(holder.Path, source)
	if holder.Path == "" || err != nil {
		return false
	}

	return current.Device == holder.Device && current.Root == filepath.Join(holder.Root, rel)
}

// sameRoot reports whether a mount's root within its filesystem is the one
// the options ask for: the whole filesystem, or the btrfs subvolume named by
// subvol. A subvolume given by subvolid cannot be compared, so any root
// matches it.
func (m *Mount) sameRoot(root string) bool {
	for _, o := range strings.Split(m.Options, ",") {
		if subvol, ok := strings.CutPrefix(o, "subvol="); ok {
			return root == filepath.Join("/", subvol)
		}

		if strings.HasPrefix(o, "subvolid=") {
			return true
		}
	}

	return root == "/"
}

// isBind reports whether the options make this a bind mount.
func (m *Mount) isBind() bool {
	for _, o := range strings.Split(m.Options, ",") {
		if o == "bind" || o == "rbind" {
			return true
		}
	}

	return false
}

// pathWithin reports whether path is dir or inside it.
func pathWithin(path, dir string) bool {
	return path == dir || dir == "/" || strings.HasPrefix(path, dir+"/")
}

// ensureDirectory creates the mount point if it does not exist.
func (m *Mount) ensureDirectory(ctx context.Context, log *viaduct.Logger, path string) error {
	env := viaduct.EnvFrom(ctx)

	info, err := os.Stat(env.Path(path))
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("mount point %s exists and is not a directory", path)
		}

		return nil
	}

	if !os.IsNotExist(err) {
		return err
	}

	if !change(ctx, log, "created-mount-point", "path", path) {
		return nil
	}

	if err := os.MkdirAll(env.Path(path), m.Mode); err != nil {
		return err
	}

	// MkdirAll applies the umask.
	return os.Chmod(env.Path(path), m.Mode)
}
