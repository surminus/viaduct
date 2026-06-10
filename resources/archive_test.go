package resources

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestTarGz(t *testing.T, files map[string]string) string {
	path := filepath.Join(t.TempDir(), "test.tar.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	for name, content := range files {
		err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	return path
}

// newTestTarGzHeaders builds a tarball from explicit headers, so tests can
// include symlink and hardlink entries. Headers are written in order, and
// content is taken from each header's PAXRecords["content"] if present.
func newTestTarGzHeaders(t *testing.T, headers []*tar.Header, contents map[string]string) string {
	path := filepath.Join(t.TempDir(), "test.tar.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	for _, hdr := range headers {
		content := contents[hdr.Name]
		hdr.Size = int64(len(content))

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}

		if content != "" {
			if _, err := tw.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
	}

	return path
}

func newTestZip(t *testing.T, files map[string]string) string {
	path := filepath.Join(t.TempDir(), "test.zip")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	return path
}

func TestArchivePreflightChecks(t *testing.T) {
	t.Run("requires path", func(t *testing.T) {
		a := &Archive{Dest: "/tmp"}

		err := a.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Path")
	})

	t.Run("requires dest", func(t *testing.T) {
		a := &Archive{Path: "/tmp/test.tar.gz"}

		err := a.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Dest")
	})

	t.Run("unrecognised format", func(t *testing.T) {
		a := &Archive{Path: "/tmp/test.rar", Dest: "/tmp"}

		err := a.PreflightChecks(testLogger)
		assert.EqualError(t, err, "unrecognised archive format: /tmp/test.rar")
	})

	t.Run("negative strip", func(t *testing.T) {
		a := &Archive{Path: "/tmp/test.tar.gz", Dest: "/tmp", Strip: -1}

		err := a.PreflightChecks(testLogger)
		assert.EqualError(t, err, "strip must not be negative")
	})
}

func TestArchive(t *testing.T) {
	t.Run("extract tar.gz with strip and pick", func(t *testing.T) {
		path := newTestTarGz(t, map[string]string{
			"node_exporter-1.7.0/node_exporter": "binary",
			"node_exporter-1.7.0/LICENSE":       "license",
		})
		dest := t.TempDir()

		a := &Archive{
			Path:  path,
			Dest:  dest,
			Strip: 1,
			Pick:  []string{"node_exporter"},
		}

		err := a.Run(testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dest, "node_exporter"))
		assert.NoError(t, err)
		assert.Equal(t, "binary", string(content))

		info, err := os.Stat(filepath.Join(dest, "node_exporter"))
		assert.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode())

		assert.NoFileExists(t, filepath.Join(dest, "LICENSE"))
	})

	t.Run("extract zip", func(t *testing.T) {
		path := newTestZip(t, map[string]string{
			"dir/file": "content",
		})
		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dest, "dir", "file"))
		assert.NoError(t, err)
		assert.Equal(t, "content", string(content))
	})

	t.Run("path traversal entries are skipped", func(t *testing.T) {
		path := newTestTarGz(t, map[string]string{
			"../evil": "evil",
			"safe":    "safe",
		})
		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testLogger)
		assert.NoError(t, err)

		assert.NoFileExists(t, filepath.Join(filepath.Dir(dest), "evil"))
		assert.FileExists(t, filepath.Join(dest, "safe"))
	})

	t.Run("not if exists", func(t *testing.T) {
		path := newTestTarGz(t, map[string]string{
			"file": "new",
		})
		dest := t.TempDir()

		err := os.WriteFile(filepath.Join(dest, "file"), []byte("old"), 0o644)
		if err != nil {
			t.Fatal(err)
		}

		a := &Archive{
			Path:        path,
			Dest:        dest,
			Pick:        []string{"file"},
			NotIfExists: true,
		}

		err = a.Run(testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dest, "file"))
		assert.NoError(t, err)
		assert.Equal(t, "old", string(content))
	})

	t.Run("unsafe symlink is skipped and cannot be written through", func(t *testing.T) {
		// A symlink escaping dest, followed by a file written through it,
		// is the classic tar-slip. The symlink must be rejected so the
		// file lands inside dest (or not at all), never outside.
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "escape", Typeflag: tar.TypeSymlink, Linkname: "../../outside", Mode: 0o777},
			{Name: "escape/pwned", Typeflag: tar.TypeReg, Mode: 0o644},
		}, map[string]string{"escape/pwned": "owned"})

		base := t.TempDir()
		dest := filepath.Join(base, "dest")

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testLogger)
		assert.NoError(t, err)

		// Nothing should have escaped to base/outside
		assert.NoDirExists(t, filepath.Join(base, "outside"))
		assert.NoFileExists(t, filepath.Join(base, "outside"))
		assert.NoFileExists(t, "escape")
		assert.NoFileExists(t, filepath.Join(filepath.Dir(base), "outside"))
	})

	t.Run("absolute symlink target is skipped", func(t *testing.T) {
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "abs", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777},
		}, nil)

		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testLogger)
		assert.NoError(t, err)
		assert.NoFileExists(t, filepath.Join(dest, "abs"))
	})

	t.Run("symlink within dest is allowed", func(t *testing.T) {
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "real", Typeflag: tar.TypeReg, Mode: 0o644},
			{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "real", Mode: 0o777},
		}, map[string]string{"real": "content"})

		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testLogger)
		assert.NoError(t, err)

		linkTarget, err := os.Readlink(filepath.Join(dest, "alias"))
		assert.NoError(t, err)
		assert.Equal(t, "real", linkTarget)
	})

	t.Run("hardlink is extracted", func(t *testing.T) {
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "original", Typeflag: tar.TypeReg, Mode: 0o644},
			{Name: "duplicate", Typeflag: tar.TypeLink, Linkname: "original", Mode: 0o644},
		}, map[string]string{"original": "shared"})

		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dest, "duplicate"))
		assert.NoError(t, err)
		assert.Equal(t, "shared", string(content))
	})
}
