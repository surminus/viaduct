package resources

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/surminus/viaduct"
)

// Archive extracts a tar or zip archive into a destination directory.
// Supported formats are tar.gz, tgz, tar.bz2, tbz2, tar and zip, detected
// by file extension.
//
// After extracting, it writes a marker holding the archive's SHA-256 along
// with Strip, Pick, Rename and PreserveOwners, in /var/lib/viaduct/archive
// when running as root, or $XDG_STATE_HOME/viaduct/archive otherwise, which
// defaults to ~/.local/state/viaduct/archive. The archive is not extracted again while
// the marker matches, Dest exists and every Pick entry exists. Removing the
// marker forces the next run to extract.
type Archive struct {
	// Path is the path to the archive file
	Path string

	// Dest is the directory to extract into
	Dest string

	// Strip removes this number of leading path components from archive
	// entries, like tar --strip-components. Optional.
	Strip int

	// Pick only extracts entries with these paths, after stripping.
	// Optional.
	Pick []string

	// Rename extracts the entries named by its keys, after stripping, to the
	// paths in Dest given by its values. Pick still lists the names in the
	// archive. Optional.
	Rename map[string]string `json:",omitempty"`

	// AllowAbsoluteLinks extracts symlinks whose target is an absolute path,
	// which otherwise fail the extraction. An entry that would be written
	// through a symlink to outside Dest still fails. Optional.
	AllowAbsoluteLinks bool `json:",omitempty"`

	// PreserveOwners gives each extracted entry the owner and group recorded
	// in the archive, by numeric ID. A directory that already exists keeps
	// its owner, and one created only to hold an entry is owned by the user
	// running viaduct. The default is to leave every entry owned by that
	// user. Requires root, and a tar archive. Optional.
	PreserveOwners bool `json:",omitempty"`

	// NotIfExists will skip extraction if all Pick entries already exist
	// within Dest, or if Dest exists when Pick is not set, without reading
	// the archive. Optional.
	NotIfExists bool
}

// Extract is a shortcut for extracting an archive into a directory
func Extract(archive, dest string) *Archive {
	return &Archive{Path: archive, Dest: dest}
}

func (a *Archive) Description() string {
	return fmt.Sprintf("%s -> %s", a.Path, a.Dest)
}

func (a *Archive) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (a *Archive) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if a.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	if a.Dest == "" {
		return fmt.Errorf("required parameter: Dest")
	}

	if a.Strip < 0 {
		return fmt.Errorf("cannot set Strip to a negative number: %d", a.Strip)
	}

	if _, err := archiveFormat(a.Path); err != nil {
		return err
	}

	if a.PreserveOwners {
		if format, _ := archiveFormat(a.Path); format == "zip" {
			return fmt.Errorf("PreserveOwners needs a tar archive, since a zip archive records no owners")
		}

		if err := requireRoot(ctx, "archive"); err != nil {
			return fmt.Errorf("%w to preserve owners; unset PreserveOwners to leave every entry owned by the running user", err)
		}
	}

	for from, to := range a.Rename {
		if !filepath.IsLocal(to) {
			return fmt.Errorf("rename of %s must be a path within Dest: %s", from, to)
		}
	}

	return nil
}

func (a *Archive) OperationName() string {
	return "Extract"
}

func (a *Archive) Run(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	apath := env.Path(viaduct.ExpandPath(a.Path))
	dest := env.Path(viaduct.ExpandPath(a.Dest))

	marker, err := archiveMarker(env, a.Path, a.Dest)
	if err != nil {
		return err
	}

	// An extraction that failed part way leaves Dest behind, so the partial
	// marker it leaves too is what stops NotIfExists taking it as finished
	partial := marker + ".partial"

	if a.NotIfExists && a.upToDate(dest) && !viaduct.FileExists(partial) {
		log.Noop("up-to-date", "path", apath, "dest", dest)
		return nil
	}

	// In a dry run the archive may not exist yet, because an earlier
	// resource would have created it, so that is reported as a change.
	// Otherwise extracting reports the missing file.
	var stamp string
	if viaduct.FileExists(apath) || !env.DryRun {
		var err error
		if stamp, err = a.stamp(apath); err != nil {
			return err
		}

		if a.extracted(dest, marker, stamp) {
			log.Noop("up-to-date", "path", apath, "dest", dest)
			return nil
		}
	}

	if env.DryRun {
		log.Info("extracted", "path", apath, "dest", dest)
		return nil
	}

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}

	// Removed first, so an extraction that fails part way is retried on the
	// next run rather than matching the old marker
	if err := os.Remove(marker); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(partial, nil, 0o644); err != nil {
		return err
	}

	count, err := a.extract(log, apath, dest)
	if err != nil {
		return err
	}

	if err := viaduct.WriteFileAtomic(marker, []byte(stamp), 0o644); err != nil {
		return err
	}

	if err := os.Remove(partial); err != nil {
		return err
	}

	log.Info("extracted", "path", apath, "dest", dest, "files", strconv.Itoa(count))

	return nil
}

// archiveMarker returns the path of the marker for an archive extracted into
// dest. It is kept outside dest so that nothing viaduct needs ends up among
// the extracted files. The name is a hash of both paths as declared, before
// the root is applied, so each archive and destination pair has its own.
func archiveMarker(env viaduct.Env, archive, dest string) (string, error) {
	dir, err := archiveStateDir(env)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(viaduct.ExpandPath(archive) + "\x00" + viaduct.ExpandPath(dest)))

	return env.Path(filepath.Join(dir, hex.EncodeToString(sum[:16]))), nil
}

// archiveStateDir is where Archive keeps its markers: under /var/lib as root,
// and in the user's XDG state directory otherwise, since an unprivileged run
// cannot write to /var/lib.
func archiveStateDir(env viaduct.Env) (string, error) {
	if env.IsRoot() {
		return "/var/lib/viaduct/archive", nil
	}

	if dir := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "viaduct", "archive"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding where to keep the archive marker: %w", err)
	}

	return filepath.Join(home, ".local", "state", "viaduct", "archive"), nil
}

// stamp is what the marker holds: the archive's digest, and the options that
// change what is extracted from it, so changing Strip or Pick extracts again
func (a *Archive) stamp(apath string) (string, error) {
	file, err := os.Open(apath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "sha256 %s\nstrip %d\n", hex.EncodeToString(hash.Sum(nil)), a.Strip)

	for _, p := range a.Pick {
		fmt.Fprintf(&b, "pick %s\n", p)
	}

	for _, from := range slices.Sorted(maps.Keys(a.Rename)) {
		fmt.Fprintf(&b, "rename %s %s\n", from, a.Rename[from])
	}

	if a.PreserveOwners {
		b.WriteString("preserve-owners\n")
	}

	return b.String(), nil
}

// extracted reports whether the marker shows this archive was extracted with
// the same options, and dest and every picked entry are still there
func (a *Archive) extracted(dest, marker, stamp string) bool {
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != stamp || !viaduct.DirExists(dest) {
		return false
	}

	for _, p := range a.Pick {
		if !viaduct.FileExists(filepath.Join(dest, a.renamed(p))) {
			return false
		}
	}

	return true
}

// renamed returns where an entry, after stripping, is extracted to within
// Dest
func (a *Archive) renamed(entry string) string {
	if to, ok := a.Rename[entry]; ok {
		return to
	}

	return entry
}

// upToDate returns true if everything we would extract already exists
func (a *Archive) upToDate(dest string) bool {
	if len(a.Pick) == 0 {
		return viaduct.DirExists(dest)
	}

	for _, p := range a.Pick {
		if !viaduct.FileExists(filepath.Join(dest, a.renamed(p))) {
			return false
		}
	}

	return true
}

func archiveFormat(p string) (string, error) {
	switch {
	case strings.HasSuffix(p, ".tar.gz"), strings.HasSuffix(p, ".tgz"):
		return "tar.gz", nil
	case strings.HasSuffix(p, ".tar.bz2"), strings.HasSuffix(p, ".tbz2"):
		return "tar.bz2", nil
	case strings.HasSuffix(p, ".tar"):
		return "tar", nil
	case strings.HasSuffix(p, ".zip"):
		return "zip", nil
	default:
		return "", fmt.Errorf("unrecognised archive format: %s", p)
	}
}

func (a *Archive) extract(log *viaduct.Logger, apath, dest string) (int, error) {
	format, err := archiveFormat(apath)
	if err != nil {
		return 0, err
	}

	// Every entry is written through root, which refuses any path that
	// resolves outside dest, by ".." or by a symlink, at the moment it is
	// used. Checking paths first and writing afterwards would leave a gap in
	// which a symlink could be swapped in.
	root, err := os.OpenRoot(dest)
	if err != nil {
		return 0, err
	}
	defer root.Close()

	if format == "zip" {
		return a.extractZip(root, apath)
	}

	file, err := os.Open(apath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var reader io.Reader = file
	switch format {
	case "tar.gz":
		gz, err := gzip.NewReader(file)
		if err != nil {
			return 0, err
		}
		defer gz.Close()
		reader = gz
	case "tar.bz2":
		reader = bzip2.NewReader(file)
	}

	return a.extractTar(log, root, reader)
}

func (a *Archive) extractTar(log *viaduct.Logger, root *os.Root, reader io.Reader) (int, error) {
	tr := tar.NewReader(reader)

	// Directories created only to hold an entry, before the archive's own
	// entry for them, which tar does not have to put first
	made := map[string]bool{}
	seen := map[string]bool{}

	var count int
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}

		if escapes(hdr.Name) {
			return count, fmt.Errorf("archive entry %s is outside Dest", hdr.Name)
		}

		rel, ok := a.target(hdr.Name)
		if !ok {
			continue
		}

		if a.PreserveOwners {
			if err := noteMissingParents(root, rel, made, seen); err != nil {
				return count, err
			}
		}

		if err := a.extractEntry(log, root, tr, hdr, rel, made); err != nil {
			return count, fmt.Errorf("extracting %s: %w", hdr.Name, err)
		}

		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeSymlink, tar.TypeLink:
			count++
		}
	}

	return count, nil
}

// noteMissingParents records in made each directory above rel that does not
// exist yet, so extracting rel creates it. seen holds the directories already
// checked.
func noteMissingParents(root *os.Root, rel string, made, seen map[string]bool) error {
	for dir := filepath.Dir(rel); dir != "." && !seen[dir]; dir = filepath.Dir(dir) {
		seen[dir] = true

		_, err := root.Lstat(dir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			made[dir] = true
		case err != nil:
			return err
		}
	}

	return nil
}

// extractEntry writes one tar entry at rel within root. made holds the
// directories that extraction created before reaching their own entry.
func (a *Archive) extractEntry(log *viaduct.Logger, root *os.Root, tr *tar.Reader, hdr *tar.Header, rel string, made map[string]bool) error {
	mode := hdr.FileInfo().Mode().Perm()

	switch hdr.Typeflag {
	case tar.TypeDir:
		// A directory that is already there, such as bin when extracting
		// into /usr/local, keeps its owner. Giving it the archive's would
		// hand everything in it to that user.
		_, err := root.Lstat(rel)
		existed := err == nil && !made[rel]

		if err := root.MkdirAll(rel, mode); err != nil {
			return err
		}

		if existed {
			return nil
		}

		return a.chown(root, rel, hdr)
	case tar.TypeReg:
		return writeEntry(root, rel, tr, mode, func(f *os.File) error {
			if !a.PreserveOwners {
				return nil
			}

			return f.Chown(hdr.Uid, hdr.Gid)
		})
	case tar.TypeSymlink:
		if err := a.linkAllowed(root, rel, hdr.Linkname); err != nil {
			return err
		}
		if err := replace(root, rel, true); err != nil {
			return err
		}
		if err := root.Symlink(hdr.Linkname, rel); err != nil {
			return err
		}

		return a.chown(root, rel, hdr)
	case tar.TypeLink:
		// Hardlinks point at another entry in the archive, which must
		// already have been extracted to be linkable
		if escapes(hdr.Linkname) {
			return fmt.Errorf("hardlink points outside Dest: %s", hdr.Linkname)
		}
		source, ok := a.target(hdr.Linkname)
		if !ok {
			return fmt.Errorf("hardlink points to %s, which was not extracted", hdr.Linkname)
		}
		info, err := root.Lstat(source)
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("hardlink points to %s, which was not extracted", hdr.Linkname)
		}
		if err != nil {
			return err
		}
		// A hardlink to a symlink is the same link in another directory, so
		// its target has to be allowed from there too
		if info.Mode()&fs.ModeSymlink != 0 {
			linkname, err := root.Readlink(source)
			if err != nil {
				return err
			}
			if err := a.linkAllowed(root, rel, linkname); err != nil {
				return err
			}
		}
		if err := replace(root, rel, true); err != nil {
			return err
		}

		return root.Link(source, rel)
	case tar.TypeXGlobalHeader:
		// pax global header, carries no file content
	default:
		log.Warn("skipped-entry", "name", hdr.Name, "type", string(rune(hdr.Typeflag)))
	}

	return nil
}

// chown gives an extracted directory or symlink the owner recorded in the
// archive, when PreserveOwners is set. A symlink is changed itself, not what
// it points to.
func (a *Archive) chown(root *os.Root, rel string, hdr *tar.Header) error {
	if !a.PreserveOwners {
		return nil
	}

	return root.Lchown(rel, hdr.Uid, hdr.Gid)
}

// linkAllowed returns an error for a symlink that points outside Dest,
// unless it is absolute and AllowAbsoluteLinks is set. Such a link cannot
// make a write escape, since root refuses to follow it, but it would lead
// anything else reading Dest outside it.
func (a *Archive) linkAllowed(root *os.Root, rel, linkname string) error {
	if filepath.IsAbs(linkname) {
		if a.AllowAbsoluteLinks {
			return nil
		}

		return fmt.Errorf("symlink points outside Dest: %s; set AllowAbsoluteLinks to extract it", linkname)
	}

	// A relative target is checked against where the link sits, which is
	// only its path in the archive when nothing above it is a symlink
	dir := filepath.Dir(rel)
	for d := dir; d != "."; d = filepath.Dir(d) {
		info, err := root.Lstat(d)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}

		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symlink is beneath another symlink, %s", d)
		}
	}

	// The target may climb only to a directory above the link, with every
	// ".." ahead of any other component. Cleaning "a/.." as text would be
	// wrong when a is a symlink, since the kernel follows a first, and a may
	// be extracted after this link. Held for every link, this means each one
	// resolves to a path that only descends from a directory in Dest.
	depth := 0
	if dir != "." {
		depth = len(strings.Split(dir, string(filepath.Separator)))
	}

	climbing := true
	for _, part := range strings.Split(linkname, "/") {
		switch part {
		case "", ".":
		case "..":
			if !climbing || depth == 0 {
				return fmt.Errorf("symlink points outside Dest: %s", linkname)
			}
			depth--
		default:
			climbing = false
		}
	}

	return nil
}

// escapes reports whether an archive entry name climbs out of the directory
// it is extracted into. Leading slashes do not count, because they are
// stripped before extracting.
func escapes(name string) bool {
	name = path.Clean(strings.TrimLeft(name, "/"))

	return name == ".." || strings.HasPrefix(name, "../")
}

func (a *Archive) extractZip(root *os.Root, apath string) (int, error) {
	zr, err := zip.OpenReader(apath)
	if err != nil {
		return 0, err
	}
	defer zr.Close()

	var count int
	for _, f := range zr.File {
		if escapes(f.Name) {
			return count, fmt.Errorf("archive entry %s is outside Dest", f.Name)
		}

		rel, ok := a.target(f.Name)
		if !ok {
			continue
		}

		if f.FileInfo().IsDir() {
			// A zip made outside Unix records a directory as read and write
			// only, which would leave it impossible to enter, so it can
			// search wherever it can be read
			mode := f.Mode().Perm()
			mode |= (mode & 0o444) >> 2

			if err := root.MkdirAll(rel, mode); err != nil {
				return count, fmt.Errorf("extracting %s: %w", f.Name, err)
			}
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return count, err
		}

		err = writeEntry(root, rel, rc, f.Mode().Perm(), nil)
		rc.Close()
		if err != nil {
			return count, fmt.Errorf("extracting %s: %w", f.Name, err)
		}

		count++
	}

	return count, nil
}

// target maps an archive entry name to a path relative to Dest, applying
// Strip, Pick and Rename. Callers reject names that escape first.
func (a *Archive) target(name string) (string, bool) {
	name = path.Clean(strings.TrimLeft(name, "/"))

	if name == "." {
		return "", false
	}

	parts := strings.Split(name, "/")
	if len(parts) <= a.Strip {
		return "", false
	}

	rel := strings.Join(parts[a.Strip:], "/")

	if len(a.Pick) > 0 && !slices.Contains(a.Pick, rel) {
		return "", false
	}

	return filepath.FromSlash(a.renamed(rel)), true
}

// replace creates the directory rel goes in and removes whatever is at rel,
// so the entry extracted there is created afresh. Writing into an existing
// file instead would follow a symlink, or change every name hardlinked to it.
// A directory is removed with everything in it only when overDir is set, for
// a link that takes its place, as when a newer version of an archive turns a
// directory into a symlink.
func replace(root *os.Root, rel string, overDir bool) error {
	if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return err
	}

	info, err := root.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return root.Remove(rel)
	}

	if !overDir {
		return fmt.Errorf("a directory is in the way")
	}

	return root.RemoveAll(rel)
}

// writeEntry creates a file at rel with the content from reader, replacing
// whatever was there. The file is created exclusively, so nothing put in its
// place after replace removed the old one is written to. setup runs on the
// open file before its mode is set, and may be nil.
func writeEntry(root *os.Root, rel string, reader io.Reader, mode os.FileMode, setup func(*os.File) error) error {
	if err := replace(root, rel, false); err != nil {
		return err
	}

	file, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}

	// nolint:gosec
	_, err = io.Copy(file, reader)
	if err == nil && setup != nil {
		err = setup(file)
	}
	if err == nil {
		// The mode given to OpenFile is reduced by the umask
		err = file.Chmod(mode)
	}

	if cerr := file.Close(); err == nil {
		err = cerr
	}

	return err
}
