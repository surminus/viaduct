package resources

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/surminus/viaduct"
)

// fstabPath is the filesystem table.
const fstabPath = "/etc/fstab"

// fstabEntry is a line in /etc/fstab
type fstabEntry struct {
	Source  string
	Path    string
	FSType  string
	Options string
	Dump    int
	Pass    int
}

// String formats the entry as a line of /etc/fstab, escaping whitespace in
// each field.
func (e fstabEntry) String() string {
	return strings.Join([]string{
		fstabEscape(e.Source),
		fstabEscape(e.Path),
		fstabEscape(e.FSType),
		fstabEscape(e.Options),
		strconv.Itoa(e.Dump),
		strconv.Itoa(e.Pass),
	}, " ")
}

// fstabEscape octal-escapes characters fstab and mountinfo cannot hold in a
// field.
func fstabEscape(s string) string {
	r := strings.NewReplacer(`\`, `\134`, " ", `\040`, "\t", `\011`, "\n", `\012`)
	return r.Replace(s)
}

// fstabUnescape reverses fstabEscape.
func fstabUnescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}

	var b strings.Builder

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3

				continue
			}
		}

		b.WriteByte(s[i])
	}

	return b.String()
}

// fstabFields returns the unescaped fields of an fstab line, or nil for a
// blank line or a comment.
func fstabFields(line string) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return nil
	}

	fields := strings.Fields(trimmed)
	for i, f := range fields {
		fields[i] = fstabUnescape(f)
	}

	return fields
}

// ensureFstabEntry replaces the first line that match selects with entry and
// removes any other matches, appending entry if none match. It reports
// whether the file changed, or in a dry run would have. Writes are atomic
// and serialised.
func ensureFstabEntry(
	ctx context.Context,
	log *viaduct.Logger,
	entry fstabEntry,
	match func(fields []string) bool,
) (bool, error) {
	env := viaduct.EnvFrom(ctx)
	path := env.Path(fstabPath)

	defer lockPath(path)()

	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}

	want := entry.String()

	var (
		lines    []string
		replaced bool
		changed  bool
	)

	if len(content) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	}

	out := make([]string, 0, len(lines)+1)

	for _, line := range lines {
		fields := fstabFields(line)
		if fields == nil || !match(fields) {
			out = append(out, line)
			continue
		}

		if replaced {
			changed = true
			continue
		}

		replaced = true

		if !sameFstabEntry(fields, entry) {
			changed = true
			line = want
		}

		out = append(out, line)
	}

	if !replaced {
		out = append(out, want)
		changed = true
	}

	if !changed {
		log.Noop("fstab-up-to-date", "path", fstabPath, "entry", want)
		return false, nil
	}

	log.Info("fstab-updated", "path", fstabPath, "entry", want)

	if env.DryRun {
		return true, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}

	if err := viaduct.WriteFileAtomic(path, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", fstabPath, err)
	}

	return true, nil
}

// sameFstabEntry reports whether a line matches the entry, treating missing
// dump and pass fields as zero, as mount does.
func sameFstabEntry(fields []string, e fstabEntry) bool {
	if len(fields) < 4 || len(fields) > 6 {
		return false
	}

	num := func(i int) int {
		if i >= len(fields) {
			return 0
		}

		n, err := strconv.Atoi(fields[i])
		if err != nil {
			return -1
		}

		return n
	}

	return fields[0] == e.Source &&
		fields[1] == e.Path &&
		fields[2] == e.FSType &&
		fields[3] == e.Options &&
		num(4) == e.Dump &&
		num(5) == e.Pass
}

// reloadSystemd runs "systemctl daemon-reload" so systemd regenerates its
// fstab units. It does nothing without systemd.
func reloadSystemd(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)

	if !viaduct.DirExists(env.Path("/run/systemd/system")) {
		return nil
	}

	if !change(ctx, log, "daemon-reload") {
		return nil
	}

	return runCommand(ctx, log, "systemctl", "daemon-reload")
}

// devicePath resolves UUID=, LABEL=, PARTUUID= and PARTLABEL= sources and
// symlinks to a device path. Anything else, or anything unresolvable, is
// returned unchanged.
func devicePath(env viaduct.Env, source string) string {
	for tag, dir := range map[string]string{
		"UUID=":      "/dev/disk/by-uuid",
		"LABEL=":     "/dev/disk/by-label",
		"PARTUUID=":  "/dev/disk/by-partuuid",
		"PARTLABEL=": "/dev/disk/by-partlabel",
	} {
		if v, ok := strings.CutPrefix(source, tag); ok {
			source = filepath.Join(dir, strings.Trim(v, `"`))
			break
		}
	}

	if !filepath.IsAbs(source) {
		return source
	}

	resolved, err := filepath.EvalSymlinks(env.Path(source))
	if err != nil {
		return source
	}

	return unroot(env, resolved)
}

// resolvePath resolves the symlinks in an absolute path, as mountinfo lists
// it. A path that does not exist yet is resolved as far as its nearest
// existing parent.
func resolvePath(env viaduct.Env, path string) string {
	if !filepath.IsAbs(path) {
		return path
	}

	dir, rest := filepath.Clean(path), ""

	for {
		if resolved, err := filepath.EvalSymlinks(env.Path(dir)); err == nil {
			return filepath.Join(unroot(env, resolved), rest)
		}

		if dir == "/" {
			return path
		}

		dir, rest = filepath.Dir(dir), filepath.Join(filepath.Base(dir), rest)
	}
}

// unroot maps a path under the environment's root back to the path it
// stands for. A path outside the root is returned as it is.
func unroot(env viaduct.Env, path string) string {
	if env.Root == "" {
		return path
	}

	rel, err := filepath.Rel(env.Root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return path
	}

	return filepath.Join("/", rel)
}

// sameDevice reports whether two sources refer to the same thing.
func sameDevice(env viaduct.Env, a, b string) bool {
	return a == b || devicePath(env, a) == devicePath(env, b)
}

// mountInfo is a line of /proc/self/mountinfo
type mountInfo struct {
	// Device is the major:minor of the filesystem
	Device string

	// Root is the directory within the filesystem that is mounted, which is
	// "/" except for a bind mount
	Root string

	Path   string
	FSType string
	Source string
}

// mounts reads /proc/self/mountinfo.
func mounts(env viaduct.Env) ([]mountInfo, error) {
	content, err := os.ReadFile(env.Path("/proc/self/mountinfo"))
	if err != nil {
		return nil, err
	}

	var out []mountInfo

	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)

		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}

		if sep < 5 || len(fields) < sep+3 {
			continue
		}

		out = append(out, mountInfo{
			Device: fields[2],
			Root:   fstabUnescape(fields[3]),
			Path:   fstabUnescape(fields[4]),
			FSType: fields[sep+1],
			Source: fstabUnescape(fields[sep+2]),
		})
	}

	return out, nil
}

// mountedAt returns what is mounted at a path, if anything. The last of
// several stacked mounts is the visible one.
func mountedAt(env viaduct.Env, path string) (mountInfo, bool, error) {
	all, err := mounts(env)
	if err != nil {
		return mountInfo{}, false, err
	}

	path = filepath.Clean(path)

	var (
		found mountInfo
		ok    bool
	)

	for _, m := range all {
		if m.Path == path {
			found, ok = m, true
		}
	}

	return found, ok, nil
}

// probe is what blkid finds on a device
type probe struct {
	// Empty is true when blkid found nothing at all on the device
	Empty bool

	// Type is the filesystem type, such as "ext4" or "swap"
	Type string

	// PartitionTable is the partition table type, such as "gpt"
	PartitionTable string

	UUID string
}

// probeDevice reads a device with "blkid -p", which bypasses the cache. The
// device must exist, as blkid reports a missing device like an empty one.
// Any other error is permanent, so an unreadable device is never formatted.
func probeDevice(ctx context.Context, log *viaduct.Logger, device string) (probe, error) {
	env := viaduct.EnvFrom(ctx)

	block, err := env.IsBlockDevice(device)
	if err != nil {
		return probe{}, fmt.Errorf("device %s: %w", device, err)
	}

	if !block {
		return probe{}, viaduct.Permanent(fmt.Errorf("%s is not a block device", device))
	}

	out, err := viaduct.Command{Name: "blkid", Args: []string{"-p", "-o", "export", device}}.Output(ctx, log)

	if code, ok := viaduct.ExitCode(err); ok && code == 2 {
		return probe{Empty: true}, nil
	}

	if err != nil {
		return probe{}, viaduct.Permanent(fmt.Errorf("blkid could not read %s: %w", device, err))
	}

	var p probe

	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		switch key {
		case "TYPE":
			p.Type = value
		case "PTTYPE":
			p.PartitionTable = value
		case "UUID":
			p.UUID = value
		}
	}

	return p, nil
}

// describe says what a probe found, for errors.
func (p probe) describe() string {
	switch {
	case p.Type != "":
		return "a filesystem of type " + p.Type
	case p.PartitionTable != "":
		return "a partition table of type " + p.PartitionTable
	case p.Empty:
		return "nothing blkid recognises"
	default:
		return "an unrecognised signature"
	}
}
