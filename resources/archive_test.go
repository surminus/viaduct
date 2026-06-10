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
}
