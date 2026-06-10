package resources

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
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

	// NotIfExists will skip extraction if all Pick entries already exist
	// within Dest, or if Dest exists when Pick is not set. Optional.
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
func (a *Archive) PreflightChecks(log *viaduct.Logger) error {
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

	return nil
}

func (a *Archive) OperationName() string {
	return "Extract"
}

func (a *Archive) Run(log *viaduct.Logger) error {
	apath := viaduct.ExpandPath(a.Path)
	dest := viaduct.ExpandPath(a.Dest)

	if viaduct.Cli.DryRun {
		log.Info("extracted", "path", apath, "dest", dest)
		return nil
	}

	if a.NotIfExists && a.upToDate(dest) {
		log.Noop("up-to-date", "path", apath, "dest", dest)
		return nil
	}

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}

	count, err := a.extract(apath, dest)
	if err != nil {
		return err
	}

	log.Info("extracted", "path", apath, "dest", dest, "files", strconv.Itoa(count))

	return nil
}

// upToDate returns true if everything we would extract already exists
func (a *Archive) upToDate(dest string) bool {
	if len(a.Pick) == 0 {
		return viaduct.DirExists(dest)
	}

	for _, p := range a.Pick {
		if !viaduct.FileExists(filepath.Join(dest, p)) {
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

func (a *Archive) extract(apath, dest string) (int, error) {
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

	return a.extractTar(reader, dest)
}

func (a *Archive) extractTar(reader io.Reader, dest string) (int, error) {
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
			if err := os.RemoveAll(target); err != nil {
				return count, err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return count, err
			}
			count++
		}
	}

	return count, nil
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

	return filepath.Join(dest, rel), true
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
