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
// with Strip, Pick and Rename, in /var/lib/viaduct/archive when running as root, or
// $XDG_STATE_HOME/viaduct/archive otherwise, which defaults to
// ~/.local/state/viaduct/archive. The archive is not extracted again while
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
	Rename map[string]string

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
		return fmt.Errorf("strip must not be negative")
	}

	if _, err := archiveFormat(a.Path); err != nil {
		return err
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

	if a.NotIfExists && a.upToDate(dest) {
		log.Noop("up-to-date", "path", apath, "dest", dest)
		return nil
	}

	marker, err := archiveMarker(env, a.Path, a.Dest)
	if err != nil {
		return err
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

	count, err := a.extract(log, apath, dest)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}

	if err := viaduct.WriteFileAtomic(marker, []byte(stamp), 0o644); err != nil {
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

	if format == "zip" {
		return a.extractZip(apath, dest)
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

	return a.extractTar(log, reader, dest)
}

func (a *Archive) extractTar(log *viaduct.Logger, reader io.Reader, dest string) (int, error) {
	tr := tar.NewReader(reader)

	var count int
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}

		target, ok := a.target(dest, hdr.Name)
		if !ok {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, hdr.FileInfo().Mode().Perm()); err != nil {
				return count, err
			}
		case tar.TypeReg:
			if err := writeFromReader(target, tr, hdr.FileInfo().Mode().Perm()); err != nil {
				return count, err
			}
			count++
		case tar.TypeSymlink:
			// Reject symlinks whose target escapes dest. Otherwise a
			// later entry could be written through the symlink to a
			// path outside the destination (a tar-slip).
			if !symlinkWithinDest(dest, target, hdr.Linkname) {
				log.Warn("skipped-unsafe-symlink", "name", hdr.Name, "target", hdr.Linkname)
				continue
			}
			if err := os.RemoveAll(target); err != nil {
				return count, err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return count, err
			}
			count++
		case tar.TypeLink:
			// Hardlinks point at another entry in the archive, which
			// must already have been extracted to be linkable.
			source, ok := a.target(dest, hdr.Linkname)
			if !ok || !viaduct.FileExists(source) {
				log.Warn("skipped-hardlink", "name", hdr.Name, "target", hdr.Linkname)
				continue
			}
			if err := os.RemoveAll(target); err != nil {
				return count, err
			}
			if err := os.Link(source, target); err != nil {
				return count, err
			}
			count++
		case tar.TypeXGlobalHeader:
			// pax global header, carries no file content
		default:
			log.Warn("skipped-entry", "name", hdr.Name, "type", string(rune(hdr.Typeflag)))
		}
	}

	return count, nil
}

// symlinkWithinDest reports whether a symlink at linkPath pointing to
// linkname would resolve to a location inside dest. Absolute targets and
// targets that climb above dest are rejected.
func symlinkWithinDest(dest, linkPath, linkname string) bool {
	// Absolute targets point outside the extraction entirely, so reject
	// them rather than reasoning about where they land.
	if filepath.IsAbs(linkname) {
		return false
	}

	resolved := filepath.Clean(filepath.Join(filepath.Dir(linkPath), linkname))

	rel, err := filepath.Rel(dest, resolved)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (a *Archive) extractZip(apath, dest string) (int, error) {
	zr, err := zip.OpenReader(apath)
	if err != nil {
		return 0, err
	}
	defer zr.Close()

	var count int
	for _, f := range zr.File {
		target, ok := a.target(dest, f.Name)
		if !ok {
			continue
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, f.Mode().Perm()); err != nil {
				return count, err
			}
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return count, err
		}

		err = writeFromReader(target, rc, f.Mode().Perm())
		rc.Close()
		if err != nil {
			return count, err
		}

		count++
	}

	return count, nil
}

// target maps an archive entry name to a destination path, applying Strip
// and Pick, and guarding against path traversal
func (a *Archive) target(dest, name string) (string, bool) {
	name = path.Clean(strings.TrimPrefix(name, "/"))

	if name == "." || name == ".." || strings.HasPrefix(name, "../") {
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

	return filepath.Join(dest, a.renamed(rel)), true
}

func writeFromReader(target string, reader io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	// nolint:gosec
	_, cerr := io.Copy(file, reader)
	if err := file.Close(); err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}

	// OpenFile only applies the mode on creation, so enforce it for
	// existing files
	return os.Chmod(target, mode)
}
